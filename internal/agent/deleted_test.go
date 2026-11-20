// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/brokerium/brokerium/internal/agentauth"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

var errDeleted = status.Error(codes.NotFound, "the node was deleted in Brokerium")

// statusServer ends the streams of an agent with mutual TLS with the given statuses in turn,
// the last one for every later stream, and answers enrollments with enroll.
type statusServer struct {
	agentv1.UnimplementedAgentServiceServer
	statuses []error
	enroll   error
	streams  atomic.Int32
}

func (s *statusServer) Connect(agentv1.AgentService_ConnectServer) error {
	n := int(s.streams.Add(1))
	return s.statuses[min(n, len(s.statuses))-1]
}

func (s *statusServer) Enroll(context.Context, *agentv1.EnrollRequest) (*agentv1.EnrollResponse, error) {
	return nil, s.enroll
}

// serverCertificate is a certificate of the CA for 127.0.0.1, sent with the CA certificate.
func (ca *testCA) serverCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "server"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.cert.Raw}, PrivateKey: key}
}

// runMTLS starts s behind mutual TLS and an agent against it, enrolled as node-1 unless token is
// set, and returns the result of Run once it returns.
func runMTLS(t *testing.T, s *statusServer, token bool) <-chan error {
	t.Helper()
	ca := newTestCA(t)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13,
		Certificates: []tls.Certificate{ca.serverCertificate(t)}, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: ca.pool()})))
	agentv1.RegisterAgentServiceServer(srv, s)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	dir := t.TempDir()
	cfg := Config{Server: lis.Addr().String(), DataDir: filepath.Join(dir, "agent"), KafkaDir: filepath.Join(dir, "kafka"),
		KafkaDataDir: filepath.Join(dir, "data")}
	if token {
		sum := sha256.Sum256(ca.cert.Raw)
		cfg.Token = "brk1.secret." + hex.EncodeToString(sum[:])
	}
	a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if !token {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err := a.saveCredentials(ca.sign(t, &key.PublicKey), key); err != nil {
			t.Fatal(err)
		}
		if err := writeFileAtomic(a.path("ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw}), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := a.saveIdentity(identity{NodeID: "node-1", NodeName: "alpha-b101", Transport: TransportMTLS}); err != nil {
			t.Fatal(err)
		}
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
	return done
}

func TestMTLSAgentStopsWhenTheNodeIsDeleted(t *testing.T) {
	t.Setenv("JAVA_HOME", t.TempDir())
	tests := []struct {
		name     string
		statuses []error
		streams  int32
	}{
		{"deleted while connected", []error{errDeleted}, 1},
		{"deleted after a server restart", []error{status.Error(codes.Unavailable, "connection refused"), errDeleted}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &statusServer{statuses: tt.statuses}
			select {
			case err := <-runMTLS(t, s, false):
				if !errors.Is(err, ErrNodeDeleted) {
					t.Fatalf("Run = %v, want ErrNodeDeleted", err)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("the agent did not stop")
			}
			if got := s.streams.Load(); got != tt.streams {
				t.Errorf("the agent opened %d stream(s), want %d", got, tt.streams)
			}
		})
	}
}

func TestMTLSAgentRetriesOtherErrors(t *testing.T) {
	t.Setenv("JAVA_HOME", t.TempDir())
	for _, st := range []error{
		status.Error(codes.Unavailable, "connection refused"),
		status.Error(codes.PermissionDenied, "certificate was replaced by a newer enrollment"),
		status.Error(codes.Unknown, "session replaced by a newer connection"),
	} {
		t.Run(status.Code(st).String(), func(t *testing.T) {
			s := &statusServer{statuses: []error{st}}
			done := runMTLS(t, s, false)
			deadline := time.After(20 * time.Second)
			for s.streams.Load() < 2 {
				select {
				case err := <-done:
					t.Fatalf("Run returned %v after %s instead of connecting again", err, status.Code(st))
				case <-deadline:
					t.Fatal("the agent did not connect again")
				case <-time.After(50 * time.Millisecond):
				}
			}
		})
	}
}

func TestEnrollmentForADeletedNode(t *testing.T) {
	t.Setenv("JAVA_HOME", t.TempDir())
	s := &statusServer{statuses: []error{nil}, enroll: errDeleted}
	select {
	case err := <-runMTLS(t, s, true):
		if !errors.Is(err, ErrNodeDeleted) {
			t.Fatalf("Run = %v, want ErrNodeDeleted", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the enrollment did not fail")
	}
}

// TestInsecureAgentTrustsOnlyASignedDeletion answers the sessions of an agent of the insecure
// transport first with the status NotFound alone, which anyone on the path could send, then with
// an answer signed by another CA, and finally with the answer of the server.
func TestInsecureAgentTrustsOnlyASignedDeletion(t *testing.T) {
	t.Setenv("JAVA_HOME", t.TempDir())
	ca, foreign := newTestCA(t), newTestCA(t)
	p := &proxyServer{ca: ca, signer: ca, secret: bytes.Repeat([]byte{7}, agentauth.SecretSize)}
	var sessions atomic.Int32
	p.connect = func(stream agentv1.AgentService_ConnectServer) error {
		in, err := stream.Recv()
		if err != nil {
			return err
		}
		signer := ca
		switch sessions.Add(1) {
		case 1:
			return errDeleted
		case 2:
			signer = foreign
		}
		answer, err := agentauth.NodeDeleted(signer.key, in.GetSessionHello())
		if err != nil {
			return err
		}
		if err := stream.Send(&agentv1.ServerMessage{Body: &agentv1.ServerMessage_NodeDeleted{NodeDeleted: answer}}); err != nil {
			return err
		}
		return errDeleted
	}
	_, done := runInsecure(t, p)
	select {
	case err := <-done:
		if !errors.Is(err, ErrNodeDeleted) {
			t.Fatalf("Run = %v, want ErrNodeDeleted", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the agent did not stop on the signed answer")
	}
	if got := sessions.Load(); got != 3 {
		t.Errorf("the agent opened %d session(s), want 3: it must retry after the unsigned and the foreign answer", got)
	}
}
