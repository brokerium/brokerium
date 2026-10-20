// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/brokerium/brokerium/internal/agentauth"
	"github.com/brokerium/brokerium/internal/auth"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

func TestConfigCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  Config
		ok   bool
	}{
		{"defaults", Config{}, true},
		{"mutual TLS", Config{Transport: TransportMTLS, TLS: TLSSystem}, true},
		{"insecure with system CAs and a CA file", Config{Transport: TransportInsecure, TLS: TLSSystem, CAFile: "/etc/proxy-ca.pem"}, true},
		{"insecure without verification", Config{Transport: TransportInsecure, TLS: TLSSkipVerify}, true},
		{"insecure in plaintext", Config{Transport: TransportInsecure, TLS: TLSNone}, true},
		{"unknown transport", Config{Transport: "tls"}, false},
		{"unknown TLS mode", Config{Transport: TransportInsecure, TLS: "verify"}, false},
		{"TLS mode with mutual TLS", Config{Transport: TransportMTLS, TLS: TLSSkipVerify}, false},
		{"CA file with mutual TLS", Config{CAFile: "/etc/proxy-ca.pem"}, false},
		{"CA file without verification", Config{Transport: TransportInsecure, TLS: TLSSkipVerify, CAFile: "/etc/proxy-ca.pem"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := tt.cfg
			if err := cfg.Check(); (err == nil) != tt.ok {
				t.Errorf("Check = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

// proxyServer plays the server behind a TLS-terminating proxy: it enrolls the agent with a
// proof of tokenHash, sends one signed GetStatus command per session and reports what the
// agent answered.
type proxyServer struct {
	agentv1.UnimplementedAgentServiceServer
	ca        *testCA
	signer    *testCA
	tokenHash string
	secret    []byte
	tamper    func(m *agentv1.ServerMessage)
	results   chan *agentv1.CommandResult
	ended     chan error
}

func (p *proxyServer) Enroll(_ context.Context, req *agentv1.EnrollRequest) (*agentv1.EnrollResponse, error) {
	if err := agentauth.VerifyProof([]byte(p.tokenHash), req); err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	resp := &agentv1.EnrollResponse{NodeId: "node-1", NodeName: "alpha-b101", CaCertDer: p.ca.cert.Raw}
	if err := agentauth.SealEnrollment(p.signer.key, req, resp, p.secret); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

func (p *proxyServer) Connect(stream agentv1.AgentService_ConnectServer) error {
	err := p.session(stream)
	p.ended <- err
	return err
}

func (p *proxyServer) session(stream agentv1.AgentService_ConnectServer) error {
	in, err := stream.Recv()
	if err != nil {
		return err
	}
	sess, accept, err := agentauth.Accept(p.ca.key, p.secret, in.GetSessionHello(), time.Now())
	if err != nil {
		return err
	}
	if err := stream.Send(&agentv1.ServerMessage{Body: &agentv1.ServerMessage_SessionAccept{SessionAccept: accept}}); err != nil {
		return err
	}
	if in, err = stream.Recv(); err != nil {
		return err
	}
	if hello, err := sess.Open(in); err != nil || hello.GetHello() == nil {
		return errors.New("the session did not start with an authenticated hello")
	}
	cmd, err := sess.Seal(&agentv1.ServerMessage{Body: &agentv1.ServerMessage_Command{Command: &agentv1.Command{Id: "c1",
		Action: &agentv1.Command_Status{Status: &agentv1.GetStatus{}}}}})
	if err != nil {
		return err
	}
	if p.tamper != nil {
		p.tamper(cmd)
	}
	if err := stream.Send(cmd); err != nil {
		return err
	}
	for {
		in, err := stream.Recv()
		if err != nil {
			return err
		}
		m, err := sess.Open(in)
		if err != nil {
			return err
		}
		if r := m.GetResult(); r != nil {
			p.results <- r
			return nil
		}
	}
}

// runInsecure starts an agent of the insecure transport against p and returns its state
// directory and the result of Run once it returns.
func runInsecure(t *testing.T, p *proxyServer) (string, <-chan error) {
	t.Helper()
	secret, hash := auth.NewToken()
	p.tokenHash = hash
	p.results, p.ended = make(chan *agentv1.CommandResult, 4), make(chan error, 4)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(srv, p)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	sum := sha256.Sum256(p.ca.cert.Raw)
	dir := t.TempDir()
	a, err := New(Config{Server: lis.Addr().String(), Token: "brk1." + secret + "." + hex.EncodeToString(sum[:]),
		Transport: TransportInsecure, TLS: TLSNone, DataDir: filepath.Join(dir, "agent"),
		KafkaDir: filepath.Join(dir, "kafka"), KafkaDataDir: filepath.Join(dir, "data")},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done, finished := make(chan error, 1), make(chan struct{})
	go func() {
		done <- a.Run(ctx)
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		<-finished
	})
	return a.cfg.DataDir, done
}

func TestInsecureTransport(t *testing.T) {
	// The status of the node runs the Java of JAVA_HOME, which must not exist here.
	t.Setenv("JAVA_HOME", t.TempDir())
	ca := newTestCA(t)
	p := &proxyServer{ca: ca, signer: ca, secret: bytes.Repeat([]byte{7}, agentauth.SecretSize)}
	dataDir, _ := runInsecure(t, p)
	select {
	case res := <-p.results:
		if res.GetCommandId() != "c1" || !res.GetOk() || res.GetStatus() == nil {
			t.Fatalf("result %v, want a successful status of c1", res)
		}
	case err := <-p.ended:
		t.Fatalf("the session ended without a result: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("no result within 20s")
	}
	secret, err := os.ReadFile(filepath.Join(dataDir, secretFile))
	if err != nil || !bytes.Equal(secret, p.secret) {
		t.Fatalf("stored node secret %x (%v), want the secret of the server", secret, err)
	}
	if info, _ := os.Stat(filepath.Join(dataDir, secretFile)); info.Mode().Perm() != 0o600 {
		t.Errorf("node secret has mode %v, want 0600", info.Mode().Perm())
	}
	var id identity
	raw, _ := os.ReadFile(filepath.Join(dataDir, "node.json"))
	if err := json.Unmarshal(raw, &id); err != nil || id.NodeID != "node-1" || id.Transport != TransportInsecure {
		t.Errorf("node.json = %s (%v), want node-1 with the insecure transport", raw, err)
	}
}

func TestInsecureTransportRejectsTamperedCommands(t *testing.T) {
	t.Setenv("JAVA_HOME", t.TempDir())
	ca := newTestCA(t)
	p := &proxyServer{ca: ca, signer: ca, secret: bytes.Repeat([]byte{7}, agentauth.SecretSize),
		tamper: func(m *agentv1.ServerMessage) {
			body := m.GetSigned().GetSignedBody()
			body[len(body)-1] ^= 1
		}}
	runInsecure(t, p)
	select {
	case res := <-p.results:
		t.Fatalf("the agent executed a tampered command: %v", res)
	case err := <-p.ended:
		if err == nil {
			t.Fatal("the session ended with a result")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the agent kept the session open after a tampered command")
	}
	select {
	case res := <-p.results:
		t.Fatalf("the agent executed a tampered command: %v", res)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestInsecureEnrollmentRejectsForeignSignature(t *testing.T) {
	t.Setenv("JAVA_HOME", t.TempDir())
	ca := newTestCA(t)
	p := &proxyServer{ca: ca, signer: newTestCA(t), secret: bytes.Repeat([]byte{7}, agentauth.SecretSize)}
	dataDir, done := runInsecure(t, p)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run succeeded with a forged enrollment response")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not fail on a forged enrollment response")
	}
	for _, f := range []string{secretFile, "node.json", "ca.crt"} {
		if _, err := os.Stat(filepath.Join(dataDir, f)); !os.IsNotExist(err) {
			t.Errorf("%s was stored after a forged enrollment response", f)
		}
	}
}
