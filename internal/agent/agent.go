// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package agent is the runtime of brokerium-agent: it enrolls with a join token, keeps a command
// stream to the server, with mutual TLS (ADR 0009) or with signed messages behind proxies that
// terminate TLS (ADR 0017), and executes a closed set of typed commands.
package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"github.com/brokerium/brokerium/internal/version"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// Transports and their TLS modes (ADR 0017).
const (
	TransportMTLS     = "mtls"
	TransportInsecure = "insecure"

	// The insecure transport verifies the TLS certificate of the proxy against the system CAs
	// and Config.CAFile, accepts any certificate, or dials in plaintext.
	TLSSystem     = "system"
	TLSSkipVerify = "skip-verify"
	TLSNone       = "none"
)

// Config of the agent.
type Config struct {
	Server string
	Token  string
	// Transport is mtls, or insecure for servers behind proxies that terminate TLS: then TLS
	// and CAFile set how the agent dials Server.
	Transport    string
	TLS          string
	CAFile       string
	DataDir      string
	KafkaDir     string
	KafkaDataDir string
	Preinstalled string
	Process      string
	// KafkaConfig adopts an existing node: Kafka runs with this server.properties, which the
	// agent never rewrites, and storage is never formatted.
	KafkaConfig string
	// KafkaUser runs Kafka under this account when the agent manages a systemd unit.
	KafkaUser string
}

// Agent is a running brokerium-agent.
type Agent struct {
	cfg   Config
	log   *slog.Logger
	kafka *Kafka
	node  identity
}

type identity struct {
	NodeID   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
	// Transport of the enrollment; agents enrolled before the insecure transport have none.
	Transport string `json:"transport,omitempty"`
}

func (id identity) transport() string {
	if id.Transport == "" {
		return TransportMTLS
	}
	return id.Transport
}

// Check validates the transport settings; empty ones take their defaults.
func (c *Config) Check() error {
	if c.Transport == "" {
		c.Transport = TransportMTLS
	}
	if c.TLS == "" {
		c.TLS = TLSSystem
	}
	switch {
	case c.Transport != TransportMTLS && c.Transport != TransportInsecure:
		return fmt.Errorf("transport must be %s or %s, not %q", TransportMTLS, TransportInsecure, c.Transport)
	case c.TLS != TLSSystem && c.TLS != TLSSkipVerify && c.TLS != TLSNone:
		return fmt.Errorf("tls must be %s, %s or %s, not %q", TLSSystem, TLSSkipVerify, TLSNone, c.TLS)
	case c.Transport == TransportMTLS && c.TLS != TLSSystem:
		return fmt.Errorf("tls %s needs the insecure transport; mutual TLS always verifies the server CA", c.TLS)
	case c.CAFile != "" && (c.Transport != TransportInsecure || c.TLS != TLSSystem):
		return errors.New("a CA file applies to the insecure transport with tls system only")
	}
	return nil
}

// New creates an agent.
func New(cfg Config, log *slog.Logger) (*Agent, error) {
	if err := cfg.Check(); err != nil {
		return nil, err
	}
	for _, d := range []string{cfg.DataDir, cfg.KafkaDir, cfg.KafkaDataDir} {
		if err := os.MkdirAll(d, 0o755); err != nil { //nolint:gosec // Kafka directories are shared with the kafka account
			return nil, err
		}
	}
	return &Agent{cfg: cfg, log: log, kafka: newKafka(cfg)}, nil
}

func (a *Agent) path(name string) string { return filepath.Join(a.cfg.DataDir, name) }

// Run enrolls if needed and keeps the command stream open until ctx ends. An agent whose
// certificate expired, or that is configured for another transport than it enrolled with,
// enrolls again when it is given a join token.
func (a *Agent) Run(ctx context.Context) error {
	enrolled := a.loadIdentity() == nil
	switched := enrolled && a.node.transport() != a.cfg.Transport
	expired := enrolled && !switched && a.cfg.Token != "" && a.node.transport() == TransportMTLS && a.certificateExpired()
	if !enrolled || switched || expired {
		if a.cfg.Token == "" {
			if switched {
				return fmt.Errorf("enrolled with the %s transport; switching to %s needs a new join token (BROKERIUM_AGENT_TOKEN)",
					a.node.transport(), a.cfg.Transport)
			}
			return errors.New("not enrolled and no join token given (BROKERIUM_AGENT_TOKEN)")
		}
		if err := a.enroll(ctx); err != nil {
			switch {
			case expired:
				return fmt.Errorf("the agent certificate expired; enrolling again needs a new join token: %w", err)
			case switched:
				return fmt.Errorf("switching to the %s transport needs a new join token: %w", a.cfg.Transport, err)
			}
			return fmt.Errorf("enrolling: %w", err)
		}
	}
	a.log.Info("agent identity", "node", a.node.NodeName, "id", a.node.NodeID, "transport", a.node.transport())
	if err := a.kafka.Resume(); err != nil {
		a.log.Warn("restarting kafka after agent restart", "error", err)
	}
	backoff := time.Second
	for {
		err := a.session(ctx)
		if ctx.Err() != nil {
			_ = a.kafka.Shutdown(2 * time.Minute)
			return nil
		}
		a.log.Warn("connection to server lost", "error", err, "retryIn", backoff)
		select {
		case <-ctx.Done():
			_ = a.kafka.Shutdown(2 * time.Minute)
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (a *Agent) loadIdentity() error {
	raw, err := os.ReadFile(a.path("node.json"))
	if err != nil {
		return err
	}
	a.node = identity{}
	if err := json.Unmarshal(raw, &a.node); err != nil {
		return err
	}
	files := []string{a.path("ca.crt")}
	if a.node.transport() == TransportInsecure {
		files = append(files, a.path(secretFile))
	} else {
		certFile, keyFile := a.credentialPaths()
		files = append(files, certFile, keyFile)
	}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			return err
		}
	}
	return nil
}

// saveIdentity stores the node the agent enrolled as; it is written last at enrollment.
func (a *Agent) saveIdentity(id identity) error {
	raw, err := json.Marshal(id)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(a.path("node.json"), raw, 0o600); err != nil {
		return err
	}
	a.node = id
	return nil
}

// removeFiles deletes credentials of another transport from the state directory.
func (a *Agent) removeFiles(names ...string) error {
	for _, name := range names {
		if err := os.Remove(a.path(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// parseToken splits a join token brk1.<secret>.<CA fingerprint>.
func parseToken(token string) (secret, caFingerprint string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "brk1" || len(parts[2]) != 64 {
		return "", "", errors.New("malformed join token")
	}
	return parts[1], strings.ToLower(parts[2]), nil
}

// enroll exchanges the join token for the credentials of the configured transport.
func (a *Agent) enroll(ctx context.Context) error {
	secret, wantCA, err := parseToken(a.cfg.Token)
	if err != nil {
		return err
	}
	if a.cfg.Transport == TransportInsecure {
		return a.enrollInsecure(ctx, secret, wantCA)
	}
	return a.enrollMTLS(ctx, wantCA)
}

// enrollMTLS verifies the server against the CA fingerprint in the token, then exchanges a CSR
// for a client certificate.
func (a *Agent) enrollMTLS(ctx context.Context, wantCA string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: host}}, key)
	if err != nil {
		return err
	}
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, //nolint:gosec // verified below against the CA fingerprint from the token
		// VerifyConnection also runs for resumed sessions, unlike VerifyPeerCertificate.
		VerifyConnection: func(cs tls.ConnectionState) error {
			raw := make([][]byte, len(cs.PeerCertificates))
			for i, c := range cs.PeerCertificates {
				raw[i] = c.Raw
			}
			return verifyChain(raw, wantCA)
		},
	}
	conn, err := grpc.NewClient(a.cfg.Server, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		return err
	}
	defer conn.Close()
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := agentv1.NewAgentServiceClient(conn).Enroll(cctx, &agentv1.EnrollRequest{
		Token: a.cfg.Token, CsrDer: csr, Hostname: host, AgentVersion: version.Version})
	if err != nil {
		return err
	}
	if err := a.saveCredentials(resp.GetCertDer(), key); err != nil {
		return err
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: resp.GetCaCertDer()})
	if err := writeFileAtomic(a.path("ca.crt"), caPEM, 0o600); err != nil {
		return err
	}
	if err := a.saveIdentity(identity{NodeID: resp.GetNodeId(), NodeName: resp.GetNodeName(), Transport: TransportMTLS}); err != nil {
		return err
	}
	return a.removeFiles(secretFile)
}

func verifyChain(raw [][]byte, wantCA string) error {
	if len(raw) < 2 {
		return errors.New("server did not present its CA certificate")
	}
	var ca *x509.Certificate
	for _, der := range raw[1:] {
		sum := sha256.Sum256(der)
		if hex.EncodeToString(sum[:]) == wantCA {
			ca, _ = x509.ParseCertificate(der)
		}
	}
	if ca == nil {
		return errors.New("server CA does not match the fingerprint in the join token")
	}
	leaf, err := x509.ParseCertificate(raw[0])
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	_, err = leaf.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	return err
}

func (a *Agent) session(ctx context.Context) error {
	if a.node.transport() == TransportInsecure {
		return a.insecureSession(ctx)
	}
	return a.mtlsSession(ctx)
}

func (a *Agent) mtlsSession(ctx context.Context) error {
	cert, err := tls.LoadX509KeyPair(a.credentialPaths())
	if err != nil {
		return err
	}
	if time.Now().After(cert.Leaf.NotAfter) {
		return fmt.Errorf("the agent certificate expired at %s; run the install command with a new join token",
			cert.Leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	caPEM, err := os.ReadFile(a.path("ca.crt"))
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	host, _, err := net.SplitHostPort(a.cfg.Server)
	if err != nil {
		return err
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, RootCAs: pool, ServerName: host}
	conn, err := grpc.NewClient(a.cfg.Server, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)), keepaliveOption)
	if err != nil {
		return err
	}
	defer conn.Close()
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := agentv1.NewAgentServiceClient(conn).Connect(sctx)
	if err != nil {
		return err
	}
	return a.serve(ctx, sctx, stream, func() { go a.keepCertificateRenewed(sctx, conn, cert.Leaf, pool) })
}

var keepaliveOption = grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second,
	PermitWithoutStream: true})

// commandStream is a Connect stream as the agent uses it: the gRPC stream with mutual TLS, a
// signedStream with the insecure transport.
type commandStream interface {
	Send(*agentv1.AgentMessage) error
	Recv() (*agentv1.ServerMessage, error)
}

// serve sends the hello and status reports and executes commands until the stream or sctx ends;
// connected runs after the hello. Commands run under ctx, so a lost connection does not cut
// them off.
func (a *Agent) serve(ctx, sctx context.Context, stream commandStream, connected func()) error {
	var sendMu sync.Mutex
	send := func(m *agentv1.AgentMessage) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(m)
	}
	if err := send(&agentv1.AgentMessage{Body: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{
		AgentVersion: version.Version, Status: a.kafka.Status()}}}); err != nil {
		return err
	}
	a.log.Info("connected to server", "server", a.cfg.Server, "transport", a.node.transport())
	if connected != nil {
		connected()
	}
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-t.C:
				_ = send(&agentv1.AgentMessage{Body: &agentv1.AgentMessage_Status{Status: a.kafka.Status()}})
			}
		}
	}()
	commands := make(chan *agentv1.Command, 16)
	go func() {
		for cmd := range commands {
			res := a.execute(ctx, cmd)
			res.Status = a.kafka.Status()
			_ = send(&agentv1.AgentMessage{Body: &agentv1.AgentMessage_Result{Result: res}})
		}
	}()
	defer close(commands)
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		if cmd := msg.GetCommand(); cmd != nil {
			commands <- cmd
		}
	}
}

// execute runs one command. Commands of a node run one after another.
func (a *Agent) execute(ctx context.Context, cmd *agentv1.Command) *agentv1.CommandResult {
	res := &agentv1.CommandResult{CommandId: cmd.GetId(), Ok: true}
	var err error
	switch act := cmd.Action.(type) {
	case *agentv1.Command_Install:
		res.Output, err = a.kafka.Install(ctx, act.Install.GetVersion(), act.Install.GetMirrorUrl())
	case *agentv1.Command_WriteConfig:
		err = a.kafka.WriteConfig(act.WriteConfig.GetProperties(), act.WriteConfig.GetHeapOpts())
	case *agentv1.Command_Format:
		res.Output, err = a.kafka.Format(ctx, act.Format.GetClusterId(), act.Format.GetMode(), act.Format.GetInitialControllers())
	case *agentv1.Command_Start:
		err = a.kafka.Start(act.Start.GetVersion())
	case *agentv1.Command_Stop:
		timeout := time.Duration(act.Stop.GetTimeoutSeconds()) * time.Second
		if timeout <= 0 {
			timeout = 2 * time.Minute
		}
		err = a.kafka.Stop(timeout)
	case *agentv1.Command_Status:
	case *agentv1.Command_Wipe:
		err = a.kafka.Wipe(act.Wipe.GetConfirm(), a.node.NodeName)
	default:
		err = errors.New("unknown command")
	}
	if err != nil {
		res.Ok, res.Error = false, err.Error()
		a.log.Warn("command failed", "id", cmd.GetId(), "error", err)
	}
	return res
}
