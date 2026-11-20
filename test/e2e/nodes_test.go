// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

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
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/brokerium/brokerium/internal/agentauth"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// TestNodeDeletion deletes agent nodes. A node that runs Kafka is refused. The test then enrolls
// and connects like an agent, deletes the node and checks that the stream ends with NotFound and
// that the agent cannot connect again:
//
//	BROKERIUM_E2E_GATEWAY           the agent gateway (default localhost:8443)
//	BROKERIUM_E2E_INSECURE_GATEWAY  the insecure agent listener, for example localhost:8444; its
//	                                agents also get a signed answer to their session hello
func TestNodeDeletion(t *testing.T) {
	c := newClient(t)
	var all struct {
		Nodes []agentHost `json:"nodes"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/nodes", nil, &all)
	refused := false
	for _, n := range all.Nodes {
		if n.Role != "broker" && n.Role != "controller" {
			continue
		}
		status, body := c.send("DELETE", "/api/v1/nodes/"+n.ID, nil)
		if status != http.StatusConflict || !strings.Contains(string(body), "node_in_use") {
			t.Errorf("deleting %s, which runs a %s: %d %s, want 409 node_in_use", n.Name, n.Role, status, body)
		}
		refused = true
		break
	}
	if !refused {
		t.Log("no node runs a broker or a controller; the refusal is not checked")
	}

	t.Run("mutual TLS", func(t *testing.T) { deleteMTLSNode(t, c, env("BROKERIUM_E2E_GATEWAY", "localhost:8443")) })
	t.Run("insecure transport", func(t *testing.T) {
		addr := os.Getenv("BROKERIUM_E2E_INSECURE_GATEWAY")
		if addr == "" {
			t.Skip("set BROKERIUM_E2E_INSECURE_GATEWAY to the insecure agent listener")
		}
		deleteInsecureNode(t, c, addr)
	})
}

// joinToken creates a join token for a node named name and returns its secret and CA fingerprint.
func joinToken(t *testing.T, c *client, name string) (token, secret, caFingerprint string) {
	t.Helper()
	var tok struct {
		Token string `json:"token"`
	}
	c.must(http.StatusCreated, "POST", "/api/v1/join-tokens", map[string]any{"nodeName": name, "ttlMinutes": 5}, &tok)
	parts := strings.Split(tok.Token, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed join token %q", tok.Token)
	}
	return tok.Token, parts[1], parts[2]
}

// waitConnected waits until the node is connected and returns it.
func waitConnected(t *testing.T, c *client, name string) agentHost {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for {
		var res struct {
			Nodes []agentHost `json:"nodes"`
		}
		c.must(http.StatusOK, "GET", "/api/v1/nodes", nil, &res)
		for _, n := range res.Nodes {
			if n.Name == name && n.Connected {
				return n
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("node %s did not connect", name)
		}
		time.Sleep(time.Second)
	}
}

// deleteConnected deletes a connected node and checks the answer and that the node is gone.
func deleteConnected(t *testing.T, c *client, n agentHost) {
	t.Helper()
	var deleted struct {
		UninstallCommand string `json:"uninstallCommand"`
	}
	c.must(http.StatusOK, "DELETE", "/api/v1/nodes/"+n.ID, nil, &deleted)
	if !strings.HasSuffix(deleted.UninstallCommand, "/api/v1/agent/uninstall.sh | sudo sh -s --") {
		t.Errorf("uninstall command %q", deleted.UninstallCommand)
	}
	var res struct {
		Nodes []agentHost `json:"nodes"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/nodes", nil, &res)
	for _, other := range res.Nodes {
		if other.ID == n.ID {
			t.Errorf("the deleted node %s is still listed: %+v", n.Name, other)
		}
	}
}

// removeAfterFailure deletes a node that a failed test left behind.
func removeAfterFailure(t *testing.T, c *client, id string) {
	if t.Failed() {
		c.do("DELETE", "/api/v1/nodes/"+id, nil, nil)
	}
}

// ended receives from a stream until it ends and returns its status.
func ended[T any](t *testing.T, recv func() (T, error)) error {
	t.Helper()
	for {
		if _, err := recv(); err != nil {
			return err
		}
	}
}

func deleteMTLSNode(t *testing.T, c *client, gateway string) {
	name := uniqueName("deleted-mtls-")
	token, _, fingerprint := joinToken(t, c, name)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: name}}, key)
	if err != nil {
		t.Fatal(err)
	}
	enrollConn, err := grpc.NewClient(gateway, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}))) //nolint:gosec // the CA is checked against the token below
	if err != nil {
		t.Fatal(err)
	}
	defer enrollConn.Close()
	resp, err := agentv1.NewAgentServiceClient(enrollConn).Enroll(ctx, &agentv1.EnrollRequest{Token: token, CsrDer: csr,
		Hostname: name, AgentVersion: "e2e"})
	if err != nil {
		t.Fatalf("enrolling: %v", err)
	}
	if sum := sha256.Sum256(resp.GetCaCertDer()); hex.EncodeToString(sum[:]) != fingerprint {
		t.Fatal("the server CA does not match the join token")
	}
	ca, err := x509.ParseCertificate(resp.GetCaCertDer())
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	host, _, err := net.SplitHostPort(gateway)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(gateway, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13,
		Certificates: []tls.Certificate{{Certificate: [][]byte{resp.GetCertDer()}, PrivateKey: key}}, RootCAs: roots, ServerName: host})))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	agents := agentv1.NewAgentServiceClient(conn)
	connect := func() agentv1.AgentService_ConnectClient {
		stream, err := agents.Connect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		hello := &agentv1.AgentMessage{Body: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{AgentVersion: "e2e",
			Status: &agentv1.NodeStatus{Hostname: name}}}}
		_ = stream.Send(hello)
		return stream
	}
	stream := connect()
	node := waitConnected(t, c, name)
	t.Cleanup(func() { removeAfterFailure(t, c, node.ID) })

	deleteConnected(t, c, node)
	if err := ended(t, stream.Recv); status.Code(err) != codes.NotFound {
		t.Errorf("the stream of the deleted node ended with %v, want NotFound", err)
	}
	if err := ended(t, connect().Recv); status.Code(err) != codes.NotFound {
		t.Errorf("connecting again: %v, want NotFound", err)
	}
}

func deleteInsecureNode(t *testing.T, c *client, addr string) {
	name := uniqueName("deleted-insecure-")
	_, secret, fingerprint := joinToken(t, c, name)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	agents := agentv1.NewAgentServiceClient(conn)
	e, err := agentauth.NewEnrollment(agentauth.TokenKey(secret), name, "e2e")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := agents.Enroll(ctx, e.Request)
	if err != nil {
		t.Fatalf("enrolling: %v", err)
	}
	ca, nodeSecret, err := e.Open(resp, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	caKey, err := agentauth.CAPublicKey(ca)
	if err != nil {
		t.Fatal(err)
	}
	// open sends a session hello and returns the stream with the hello and the answer to it.
	open := func() (agentv1.AgentService_ConnectClient, *agentv1.SessionHello, *agentv1.ServerMessage) {
		stream, err := agents.Connect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		hello := agentauth.NewHello(resp.GetNodeId(), nodeSecret, time.Now())
		if err := stream.Send(&agentv1.AgentMessage{Body: &agentv1.AgentMessage_SessionHello{SessionHello: hello}}); err != nil {
			t.Fatal(err)
		}
		answer, err := stream.Recv()
		if err != nil {
			t.Fatalf("answer to the session hello: %v", err)
		}
		return stream, hello, answer
	}
	stream, hello, answer := open()
	session, err := agentauth.Join(caKey, nodeSecret, hello, answer.GetSessionAccept())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := session.Seal(&agentv1.AgentMessage{Body: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{AgentVersion: "e2e",
		Status: &agentv1.NodeStatus{Hostname: name}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(sealed); err != nil {
		t.Fatal(err)
	}
	node := waitConnected(t, c, name)
	t.Cleanup(func() { removeAfterFailure(t, c, node.ID) })

	deleteConnected(t, c, node)
	if err := ended(t, stream.Recv); status.Code(err) != codes.NotFound {
		t.Errorf("the stream of the deleted node ended with %v, want NotFound", err)
	}
	stream, hello, answer = open()
	if err := agentauth.VerifyNodeDeleted(caKey, hello, answer.GetNodeDeleted()); err != nil {
		t.Errorf("answer to the session hello of the deleted node %v: %v", answer, err)
	}
	if err := ended(t, stream.Recv); status.Code(err) != codes.NotFound {
		t.Errorf("the stream after the answer ended with %v, want NotFound", err)
	}
}
