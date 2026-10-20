// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/brokerium/brokerium/internal/agentauth"
	"github.com/brokerium/brokerium/internal/version"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// secretFile holds the node secret of the insecure transport.
const secretFile = "node.secret"

// insecureCredentials are the transport credentials for the proxy in front of the server. The
// server is authenticated by the signatures of its messages, not by TLS.
func (a *Agent) insecureCredentials() (credentials.TransportCredentials, error) {
	switch a.cfg.TLS {
	case TLSNone:
		return insecure.NewCredentials(), nil
	case TLSSkipVerify:
		return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}), nil //nolint:gosec // the insecure transport signs its messages (ADR 0017)
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("loading the system CA certificates: %w", err)
	}
	if a.cfg.CAFile != "" {
		data, err := os.ReadFile(a.cfg.CAFile)
		if err != nil {
			return nil, err
		}
		if !roots.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("%s contains no PEM certificate", a.cfg.CAFile)
		}
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}), nil
}

func (a *Agent) dialInsecure(opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	creds, err := a.insecureCredentials()
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(a.cfg.Server, append(opts, grpc.WithTransportCredentials(creds))...)
}

// enrollInsecure proves the join token without sending it and receives the node secret,
// encrypted to a new X25519 key and signed by the CA that the token pins.
func (a *Agent) enrollInsecure(ctx context.Context, tokenSecret, wantCA string) error {
	host, _ := os.Hostname()
	e, err := agentauth.NewEnrollment(agentauth.TokenKey(tokenSecret), host, version.Version)
	if err != nil {
		return err
	}
	conn, err := a.dialInsecure()
	if err != nil {
		return err
	}
	defer conn.Close()
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := agentv1.NewAgentServiceClient(conn).Enroll(cctx, e.Request)
	if err != nil {
		return err
	}
	ca, secret, err := e.Open(resp, wantCA)
	if err != nil {
		return fmt.Errorf("verifying the enrollment response: %w", err)
	}
	if err := writeFileAtomic(a.path(secretFile), secret, 0o600); err != nil {
		return err
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
	if err := writeFileAtomic(a.path("ca.crt"), caPEM, 0o600); err != nil {
		return err
	}
	if err := a.saveIdentity(identity{NodeID: resp.GetNodeId(), NodeName: resp.GetNodeName(), Transport: TransportInsecure}); err != nil {
		return err
	}
	return a.removeFiles(credentialsFile, legacyCertFile, legacyKeyFile)
}

// insecureSession opens a session with the node secret and serves it: every message is
// authenticated, whatever the connection to the proxy is.
func (a *Agent) insecureSession(ctx context.Context) error {
	secret, err := os.ReadFile(a.path(secretFile))
	if err != nil {
		return err
	}
	if len(secret) != agentauth.SecretSize {
		return fmt.Errorf("%s is damaged; run the install command with a new join token", a.path(secretFile))
	}
	ca, err := a.loadCA()
	if err != nil {
		return err
	}
	caKey, err := agentauth.CAPublicKey(ca)
	if err != nil {
		return err
	}
	conn, err := a.dialInsecure(keepaliveOption)
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
	hello := agentauth.NewHello(a.node.NodeID, secret, time.Now())
	if err := stream.Send(&agentv1.AgentMessage{Body: &agentv1.AgentMessage_SessionHello{SessionHello: hello}}); err != nil {
		return err
	}
	in, err := stream.Recv()
	if err != nil {
		return err
	}
	sess, err := agentauth.Join(caKey, secret, hello, in.GetSessionAccept())
	if err != nil {
		return fmt.Errorf("verifying the server: %w", err)
	}
	return a.serve(ctx, sctx, &signedStream{stream: stream, session: sess}, nil)
}

func (a *Agent) loadCA() (*x509.Certificate, error) {
	data, err := os.ReadFile(a.path("ca.crt"))
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("ca.crt is not PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}

// signedStream is the agent side of a Connect stream of the insecure transport: it
// authenticates every message for the server and verifies every server message before the
// agent decodes and executes it.
type signedStream struct {
	stream  agentv1.AgentService_ConnectClient
	session *agentauth.AgentSession
}

func (s *signedStream) Send(m *agentv1.AgentMessage) error {
	sealed, err := s.session.Seal(m)
	if err != nil {
		return err
	}
	return s.stream.Send(sealed)
}

func (s *signedStream) Recv() (*agentv1.ServerMessage, error) {
	in, err := s.stream.Recv()
	if err != nil {
		return nil, err
	}
	cmd, err := s.session.Open(in)
	if err != nil {
		return nil, fmt.Errorf("rejected a server message, closing the session: %w", err)
	}
	return &agentv1.ServerMessage{Body: &agentv1.ServerMessage_Command{Command: cmd}}, nil
}
