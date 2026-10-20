// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentauth

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/brokerium/brokerium/internal/auth"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

type testCA struct {
	key         *ecdsa.PrivateKey
	der         []byte
	fingerprint string
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return &testCA{key: key, der: der, fingerprint: hex.EncodeToString(sum[:])}
}

func TestInput(t *testing.T) {
	t.Parallel()
	got := input("d", []byte("ab"), nil, u64(1))
	want := []byte{0, 0, 0, 1, 'd', 0, 0, 0, 2, 'a', 'b', 0, 0, 0, 0, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 1}
	if !bytes.Equal(got, want) {
		t.Fatalf("input = %v, want %v", got, want)
	}
	// The same bytes split into other fields, or moved into the domain, encode differently.
	distinct := map[string][]byte{
		"ab|c":  input("d", []byte("ab"), []byte("c")),
		"a|bc":  input("d", []byte("a"), []byte("bc")),
		"abc":   input("d", []byte("abc")),
		"da|bc": input("da", []byte("bc")),
		"ab|c|": input("d", []byte("ab"), []byte("c"), nil),
	}
	seen := map[string]string{}
	for name, b := range distinct {
		if other, ok := seen[string(b)]; ok {
			t.Errorf("%s and %s have the same input", name, other)
		}
		seen[string(b)] = name
	}
}

func TestTokenKeyIsTheStoredHash(t *testing.T) {
	t.Parallel()
	for _, secret := range []string{"", "s3cr3t", "M9pGx0rQ2yTnV1cR6kLw-8_ZbHdUeJfA3sXoYiN4qE"} {
		if got, want := string(TokenKey(secret)), auth.HashToken(secret); got != want {
			t.Errorf("TokenKey(%q) = %s, want the stored hash %s", secret, got, want)
		}
	}
}

func TestEnrollmentProof(t *testing.T) {
	t.Parallel()
	key := TokenKey("token-secret")
	other := TokenKey("another-token")
	mitm, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		key    []byte
		mutate func(req *agentv1.EnrollRequest)
		ok     bool
	}{
		{"valid", key, func(*agentv1.EnrollRequest) {}, true},
		{"wrong token", other, func(*agentv1.EnrollRequest) {}, false},
		{"lookup of another token", key, func(r *agentv1.EnrollRequest) { r.Proof.TokenLookup = TokenLookup(other) }, false},
		{"public key replaced", key, func(r *agentv1.EnrollRequest) { r.Proof.PublicKey = mitm.PublicKey().Bytes() }, false},
		{"public key replaced and MAC with another key", key, func(r *agentv1.EnrollRequest) {
			r.Proof.PublicKey = mitm.PublicKey().Bytes()
			r.Proof.Mac = enrollMAC(other, r)
		}, false},
		{"hostname changed", key, func(r *agentv1.EnrollRequest) { r.Hostname = "elsewhere" }, false},
		{"agent version changed", key, func(r *agentv1.EnrollRequest) { r.AgentVersion = "0.0.1" }, false},
		{"nonce changed", key, func(r *agentv1.EnrollRequest) { r.Proof.Nonce = random(NonceSize) }, false},
		{"short nonce", key, func(r *agentv1.EnrollRequest) {
			r.Proof.Nonce = r.Proof.Nonce[:8]
			r.Proof.Mac = enrollMAC(key, r)
		}, false},
		{"no proof", key, func(r *agentv1.EnrollRequest) { r.Proof = nil }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, err := NewEnrollment(key, "kafka-1", "0.3.0")
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(e.Request)
			if err := VerifyProof(tt.key, e.Request); (err == nil) != tt.ok {
				t.Errorf("VerifyProof = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

// enrollment returns a verified enrollment and the response of the server to it.
func enrollment(t *testing.T, ca *testCA, secret []byte) (*Enrollment, *agentv1.EnrollResponse) {
	t.Helper()
	key := TokenKey("token-secret")
	e, err := NewEnrollment(key, "kafka-1", "0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProof(key, e.Request); err != nil {
		t.Fatal(err)
	}
	resp := &agentv1.EnrollResponse{NodeId: "0123456789abcdef", NodeName: "kafka-1", CaCertDer: ca.der}
	if err := SealEnrollment(ca.key, e.Request, resp, secret); err != nil {
		t.Fatal(err)
	}
	return e, resp
}

func TestEnrollmentResponse(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	foreign := newTestCA(t)
	secret := random(SecretSize)
	tests := []struct {
		name        string
		fingerprint string
		mutate      func(t *testing.T, e *Enrollment, resp *agentv1.EnrollResponse)
		ok          bool
	}{
		{"valid", ca.fingerprint, func(*testing.T, *Enrollment, *agentv1.EnrollResponse) {}, true},
		{"fingerprint of another CA", foreign.fingerprint, func(*testing.T, *Enrollment, *agentv1.EnrollResponse) {}, false},
		{"another CA certificate", ca.fingerprint, func(_ *testing.T, _ *Enrollment, r *agentv1.EnrollResponse) { r.CaCertDer = foreign.der }, false},
		{"signed by a foreign key", ca.fingerprint, func(t *testing.T, e *Enrollment, r *agentv1.EnrollResponse) {
			var err error
			r.Signature, err = sign(foreign.key, enrollResponseInput(e.Request.GetProof().GetMac(), r))
			if err != nil {
				t.Fatal(err)
			}
		}, false},
		{"node id changed", ca.fingerprint, func(_ *testing.T, _ *Enrollment, r *agentv1.EnrollResponse) { r.NodeId = "fedcba9876543210" }, false},
		{"node name changed", ca.fingerprint, func(_ *testing.T, _ *Enrollment, r *agentv1.EnrollResponse) { r.NodeName = "kafka-2" }, false},
		{"ciphertext changed", ca.fingerprint, func(_ *testing.T, _ *Enrollment, r *agentv1.EnrollResponse) {
			r.NodeSecret.Ciphertext[0] ^= 1
		}, false},
		{"server key replaced", ca.fingerprint, func(_ *testing.T, _ *Enrollment, r *agentv1.EnrollResponse) {
			mitm, _ := ecdh.X25519().GenerateKey(rand.Reader)
			r.NodeSecret.PublicKey = mitm.PublicKey().Bytes()
		}, false},
		{"answer to another request", ca.fingerprint, func(t *testing.T, e *Enrollment, _ *agentv1.EnrollResponse) {
			other, err := NewEnrollment(TokenKey("token-secret"), "kafka-1", "0.3.0")
			if err != nil {
				t.Fatal(err)
			}
			e.Request, e.key = other.Request, other.key
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, resp := enrollment(t, ca, secret)
			tt.mutate(t, e, resp)
			gotCA, got, err := e.Open(resp, tt.fingerprint)
			if (err == nil) != tt.ok {
				t.Fatalf("Open = %v, want ok=%v", err, tt.ok)
			}
			if tt.ok && (!bytes.Equal(got, secret) || !bytes.Equal(gotCA.Raw, ca.der)) {
				t.Error("Open returned another secret or CA")
			}
		})
	}
}

func TestNodeSecretEncryption(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	secret := random(SecretSize)
	e, resp := enrollment(t, ca, secret)
	if bytes.Contains(resp.GetNodeSecret().GetCiphertext(), secret) {
		t.Fatal("the response contains the node secret in clear")
	}
	got, err := openSecret(e.key, resp.GetNodeId(), resp.GetNodeSecret())
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("openSecret = %x, %v; want the node secret", got, err)
	}
	stranger, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openSecret(stranger, resp.GetNodeId(), resp.GetNodeSecret()); err == nil {
		t.Error("another X25519 key decrypted the node secret")
	}
	if _, err := openSecret(e.key, "fedcba9876543210", resp.GetNodeSecret()); err == nil {
		t.Error("the node secret decrypted for another node id")
	}
	e2, resp2 := enrollment(t, ca, secret)
	if bytes.Equal(resp.GetNodeSecret().GetCiphertext(), resp2.GetNodeSecret().GetCiphertext()) {
		t.Error("two enrollments produced the same ciphertext")
	}
	if _, err := openSecret(e2.key, resp.GetNodeId(), resp.GetNodeSecret()); err == nil {
		t.Error("the key of another enrollment decrypted the node secret")
	}
}

func TestSessionHello(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	secret := random(SecretSize)
	now := time.Now()
	tests := []struct {
		name   string
		secret []byte
		at     time.Time
		mutate func(h *agentv1.SessionHello)
		ok     bool
	}{
		{"valid", secret, now, func(*agentv1.SessionHello) {}, true},
		{"agent clock 4 minutes behind", secret, now.Add(-4 * time.Minute), func(*agentv1.SessionHello) {}, true},
		{"wrong node secret", random(SecretSize), now, func(*agentv1.SessionHello) {}, false},
		{"agent clock 6 minutes ahead", secret, now.Add(6 * time.Minute), func(*agentv1.SessionHello) {}, false},
		{"agent clock 6 minutes behind", secret, now.Add(-6 * time.Minute), func(*agentv1.SessionHello) {}, false},
		{"another node", secret, now, func(h *agentv1.SessionHello) { h.NodeId = "fedcba9876543210" }, false},
		{"timestamp changed", secret, now, func(h *agentv1.SessionHello) { h.TimestampMs++ }, false},
		{"nonce changed", secret, now, func(h *agentv1.SessionHello) { h.AgentNonce = random(NonceSize) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hello := NewHello("0123456789abcdef", tt.secret, tt.at)
			tt.mutate(hello)
			srv, accept, err := Accept(ca.key, secret, hello, now)
			if (err == nil) != tt.ok {
				t.Fatalf("Accept = %v, want ok=%v", err, tt.ok)
			}
			if !tt.ok {
				return
			}
			if _, err := Join(&ca.key.PublicKey, secret, hello, accept); err != nil {
				t.Fatalf("Join: %v", err)
			}
			if !bytes.Equal(srv.serverNonce, accept.GetServerNonce()) || len(srv.serverNonce) != NonceSize {
				t.Error("the session does not use the server nonce of its answer")
			}
		})
	}
}

func TestSessionAccept(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	foreign := newTestCA(t)
	secret := random(SecretSize)
	hello := NewHello("0123456789abcdef", secret, time.Now())
	_, accept, err := Accept(ca.key, secret, hello, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Join(&ca.key.PublicKey, secret, hello, accept); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := Join(&foreign.key.PublicKey, secret, hello, accept); err == nil {
		t.Error("an answer of another CA was accepted")
	}
	changed := proto.Clone(accept).(*agentv1.SessionAccept)
	changed.ServerNonce = random(NonceSize)
	if _, err := Join(&ca.key.PublicKey, secret, hello, changed); err == nil {
		t.Error("an answer with a replaced server nonce was accepted")
	}
	again := NewHello("0123456789abcdef", secret, time.Now())
	if _, err := Join(&ca.key.PublicKey, secret, again, accept); err == nil {
		t.Error("the answer to an earlier hello was accepted")
	}
	if _, err := Join(&ca.key.PublicKey, secret, hello, nil); err == nil {
		t.Error("a missing answer was accepted")
	}
}

// sessions opens a session and returns both of its sides.
func sessions(t *testing.T, ca *testCA, nodeID string, secret []byte) (*ServerSession, *AgentSession) {
	t.Helper()
	hello := NewHello(nodeID, secret, time.Now())
	srv, accept, err := Accept(ca.key, secret, hello, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Join(&ca.key.PublicKey, secret, hello, accept)
	if err != nil {
		t.Fatal(err)
	}
	return srv, agent
}

func command(id string) *agentv1.ServerMessage {
	return &agentv1.ServerMessage{Body: &agentv1.ServerMessage_Command{Command: &agentv1.Command{Id: id,
		Action: &agentv1.Command_Stop{Stop: &agentv1.StopKafka{TimeoutSeconds: 120}}}}}
}

func TestCommandSignatures(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	foreign := newTestCA(t)
	secret := random(SecretSize)
	seal := func(t *testing.T, s *ServerSession, id string) *agentv1.ServerMessage {
		t.Helper()
		m, err := s.Seal(command(id))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	// Every case delivers messages to the agent side of a session: all but the last must be
	// accepted, the last as given by ok.
	tests := []struct {
		name     string
		messages func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage
		ok       bool
	}{
		{"in order", func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage {
			return []*agentv1.ServerMessage{seal(t, srv, "c1"), seal(t, srv, "c2"), seal(t, srv, "c3")}
		}, true},
		{"tampered signed_body", func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage {
			m := seal(t, srv, "c1")
			body := m.GetSigned().GetSignedBody()
			body[len(body)-1] ^= 1
			return []*agentv1.ServerMessage{m}
		}, false},
		{"body of another command", func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage {
			m := seal(t, srv, "c1")
			body, err := marshal(command("c9"))
			if err != nil {
				t.Fatal(err)
			}
			m.GetSigned().SignedBody = body
			return []*agentv1.ServerMessage{m}
		}, false},
		{"replayed", func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage {
			m := seal(t, srv, "c1")
			return []*agentv1.ServerMessage{m, m}
		}, false},
		{"dropped", func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage {
			first := seal(t, srv, "c1")
			seal(t, srv, "c2")
			return []*agentv1.ServerMessage{first, seal(t, srv, "c3")}
		}, false},
		{"second before the first", func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage {
			seal(t, srv, "c1")
			return []*agentv1.ServerMessage{seal(t, srv, "c2")}
		}, false},
		{"seq changed", func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage {
			seal(t, srv, "c1")
			m := seal(t, srv, "c2")
			m.GetSigned().Seq = 1
			return []*agentv1.ServerMessage{m}
		}, false},
		{"from an earlier session", func(t *testing.T, _ *ServerSession) []*agentv1.ServerMessage {
			earlier, _ := sessions(t, ca, "0123456789abcdef", secret)
			return []*agentv1.ServerMessage{seal(t, earlier, "c1")}
		}, false},
		{"with another server nonce", func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage {
			old := *srv
			old.serverNonce = random(NonceSize)
			return []*agentv1.ServerMessage{seal(t, &old, "c1")}
		}, false},
		{"for another node", func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage {
			other := *srv
			other.nodeID = "fedcba9876543210"
			return []*agentv1.ServerMessage{seal(t, &other, "c1")}
		}, false},
		{"signed by a foreign key", func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage {
			forged := *srv
			forged.key = foreign.key
			return []*agentv1.ServerMessage{seal(t, &forged, "c1")}
		}, false},
		{"unsigned command", func(*testing.T, *ServerSession) []*agentv1.ServerMessage {
			return []*agentv1.ServerMessage{command("c1")}
		}, false},
		{"signed envelope inside", func(t *testing.T, srv *ServerSession) []*agentv1.ServerMessage {
			inner := seal(t, srv, "c1")
			body, err := marshal(inner)
			if err != nil {
				t.Fatal(err)
			}
			sig, err := sign(ca.key, srv.input(domainCommand, 1, body))
			if err != nil {
				t.Fatal(err)
			}
			return []*agentv1.ServerMessage{{Body: &agentv1.ServerMessage_Signed{Signed: &agentv1.SignedMessage{
				Seq: 1, SignedBody: body, Signature: sig}}}}
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, agent := sessions(t, ca, "0123456789abcdef", secret)
			msgs := tt.messages(t, srv)
			for i, m := range msgs {
				cmd, err := agent.Open(m)
				last := i == len(msgs)-1
				if ok := err == nil; ok != (tt.ok || !last) {
					t.Fatalf("message %d: Open = %v, want ok=%v", i+1, err, tt.ok || !last)
				}
				if err == nil && cmd.GetStop().GetTimeoutSeconds() != 120 {
					t.Errorf("message %d: Open returned %v", i+1, cmd)
				}
			}
		})
	}
}

func status(pid int64) *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Body: &agentv1.AgentMessage_Status{Status: &agentv1.NodeStatus{Running: true, Pid: pid}}}
}

func TestAgentMessageMACs(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	secret := random(SecretSize)
	seal := func(t *testing.T, a *AgentSession, pid int64) *agentv1.AgentMessage {
		t.Helper()
		m, err := a.Seal(status(pid))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	tests := []struct {
		name     string
		messages func(t *testing.T, agent *AgentSession) []*agentv1.AgentMessage
		ok       bool
	}{
		{"in order", func(t *testing.T, agent *AgentSession) []*agentv1.AgentMessage {
			return []*agentv1.AgentMessage{seal(t, agent, 1), seal(t, agent, 2)}
		}, true},
		{"tampered signed_body", func(t *testing.T, agent *AgentSession) []*agentv1.AgentMessage {
			m := seal(t, agent, 1)
			body := m.GetSigned().GetSignedBody()
			body[len(body)-1] ^= 1
			return []*agentv1.AgentMessage{m}
		}, false},
		{"replayed", func(t *testing.T, agent *AgentSession) []*agentv1.AgentMessage {
			m := seal(t, agent, 1)
			return []*agentv1.AgentMessage{m, m}
		}, false},
		{"dropped", func(t *testing.T, agent *AgentSession) []*agentv1.AgentMessage {
			first := seal(t, agent, 1)
			seal(t, agent, 2)
			return []*agentv1.AgentMessage{first, seal(t, agent, 3)}
		}, false},
		{"from an earlier session", func(t *testing.T, _ *AgentSession) []*agentv1.AgentMessage {
			_, earlier := sessions(t, ca, "0123456789abcdef", secret)
			return []*agentv1.AgentMessage{seal(t, earlier, 1)}
		}, false},
		{"with another agent nonce", func(t *testing.T, agent *AgentSession) []*agentv1.AgentMessage {
			old := *agent
			old.agentNonce = random(NonceSize)
			return []*agentv1.AgentMessage{seal(t, &old, 1)}
		}, false},
		{"from another node", func(t *testing.T, agent *AgentSession) []*agentv1.AgentMessage {
			other := *agent
			other.nodeID = "fedcba9876543210"
			return []*agentv1.AgentMessage{seal(t, &other, 1)}
		}, false},
		{"MAC with another node secret", func(t *testing.T, agent *AgentSession) []*agentv1.AgentMessage {
			forged := *agent
			forged.secret = random(SecretSize)
			return []*agentv1.AgentMessage{seal(t, &forged, 1)}
		}, false},
		{"unauthenticated status", func(*testing.T, *AgentSession) []*agentv1.AgentMessage {
			return []*agentv1.AgentMessage{status(1)}
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, agent := sessions(t, ca, "0123456789abcdef", secret)
			msgs := tt.messages(t, agent)
			for i, m := range msgs {
				inner, err := srv.Open(m)
				last := i == len(msgs)-1
				if ok := err == nil; ok != (tt.ok || !last) {
					t.Fatalf("message %d: Open = %v, want ok=%v", i+1, err, tt.ok || !last)
				}
				if err == nil && !inner.GetStatus().GetRunning() {
					t.Errorf("message %d: Open returned %v", i+1, inner)
				}
			}
		})
	}
}

func TestSealRefusesEnvelopes(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	secret := random(SecretSize)
	srv, agent := sessions(t, ca, "0123456789abcdef", secret)
	if _, err := srv.Seal(&agentv1.ServerMessage{Body: &agentv1.ServerMessage_SessionAccept{SessionAccept: &agentv1.SessionAccept{}}}); err == nil {
		t.Error("the server sealed a session answer")
	}
	if _, err := agent.Seal(&agentv1.AgentMessage{Body: &agentv1.AgentMessage_SessionHello{SessionHello: &agentv1.SessionHello{}}}); err == nil {
		t.Error("the agent sealed a session hello")
	}
}
