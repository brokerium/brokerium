// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package agent is the runtime of brokerium-agent: it enrolls with a join token, keeps a mutual
// TLS command stream to the server and executes a closed set of typed commands (ADR 0009).
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

// Config of the agent.
type Config struct {
	Server       string
	Token        string
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
}

// New creates an agent.
func New(cfg Config, log *slog.Logger) (*Agent, error) {
	for _, d := range []string{cfg.DataDir, cfg.KafkaDir, cfg.KafkaDataDir} {
		if err := os.MkdirAll(d, 0o755); err != nil { //nolint:gosec // Kafka directories are shared with the kafka account
			return nil, err
		}
	}
	return &Agent{cfg: cfg, log: log, kafka: newKafka(cfg)}, nil
}

func (a *Agent) path(name string) string { return filepath.Join(a.cfg.DataDir, name) }

// Run enrolls if needed and keeps the command stream open until ctx ends.
func (a *Agent) Run(ctx context.Context) error {
	if err := a.loadIdentity(); err != nil {
		if a.cfg.Token == "" {
			return errors.New("not enrolled and no join token given (BROKERIUM_AGENT_TOKEN)")
		}
		if err := a.enroll(ctx); err != nil {
			return fmt.Errorf("enrolling: %w", err)
		}
	}
	a.log.Info("agent identity", "node", a.node.NodeName, "id", a.node.NodeID)
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
	if err := json.Unmarshal(raw, &a.node); err != nil {
		return err
	}
	for _, f := range []string{"agent.crt", "agent.key", "ca.crt"} {
		if _, err := os.Stat(a.path(f)); err != nil {
			return err
		}
	}
	return nil
}

// enroll verifies the server against the CA fingerprint in the token, then exchanges a CSR for
// a client certificate.
func (a *Agent) enroll(ctx context.Context) error {
	parts := strings.Split(a.cfg.Token, ".")
	if len(parts) != 3 || parts[0] != "brk1" || len(parts[2]) != 64 {
		return errors.New("malformed join token")
	}
	wantCA := strings.ToLower(parts[2])
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
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"agent.key": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		"agent.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: resp.GetCertDer()}),
		"ca.crt":    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: resp.GetCaCertDer()}),
	}
	for name, data := range files {
		if err := os.WriteFile(a.path(name), data, 0o600); err != nil {
			return err
		}
	}
	a.node = identity{NodeID: resp.GetNodeId(), NodeName: resp.GetNodeName()}
	raw, _ := json.Marshal(a.node)
	return os.WriteFile(a.path("node.json"), raw, 0o600)
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
	cert, err := tls.LoadX509KeyPair(a.path("agent.crt"), a.path("agent.key"))
	if err != nil {
		return err
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
	conn, err := grpc.NewClient(a.cfg.Server, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second, PermitWithoutStream: true}))
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
	a.log.Info("connected to server", "server", a.cfg.Server)
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
