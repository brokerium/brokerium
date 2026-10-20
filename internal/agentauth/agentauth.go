// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package agentauth authenticates the messages of the insecure agent transport (ADR 0017),
// which does not trust the TLS between agent and server: the enrollment proof of a join token,
// the node secret the server returns encrypted, and the signatures and MACs of session
// messages. Agent and server share it, so both compute the same inputs.
package agentauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// Every MAC and signature input starts with one of these domains.
const (
	domainEnroll         = "brokerium-enroll-v1"
	domainEnrollResponse = "brokerium-enroll-response-v1"
	domainHello          = "brokerium-hello-v1"
	domainSession        = "brokerium-session-v1"
	domainCommand        = "brokerium-command-v1"
	domainAgent          = "brokerium-agent-v1"
)

// hkdfInfo derives the key that encrypts the node secret at enrollment.
const hkdfInfo = "brokerium-node-secret-v1"

const (
	// NonceSize is the size of enrollment and session nonces.
	NonceSize = 16
	// SecretSize is the size of a node secret.
	SecretSize = 32
	// MaxClockSkew is how far the time of a session hello may be from the server clock.
	MaxClockSkew = 5 * time.Minute
)

// input is what a MAC or signature covers: the domain, then every field, each as a 4-byte
// big-endian length followed by its bytes, so different field lists never share an encoding.
func input(domain string, fields ...[]byte) []byte {
	size := 4 + len(domain)
	for _, f := range fields {
		size += 4 + len(f)
	}
	out := appendField(make([]byte, 0, size), []byte(domain))
	for _, f := range fields {
		out = appendField(out, f)
	}
	return out
}

func appendField(b, field []byte) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(field))) //nolint:gosec // gRPC limits messages far below 4 GiB
	return append(b, field...)
}

// u64 encodes a number as an input field: 8 bytes, big-endian.
func u64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

func mac(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sign(key *ecdsa.PrivateKey, data []byte) ([]byte, error) {
	sum := sha256.Sum256(data)
	return ecdsa.SignASN1(rand.Reader, key, sum[:])
}

func verify(key *ecdsa.PublicKey, data, signature []byte) bool {
	sum := sha256.Sum256(data)
	return ecdsa.VerifyASN1(key, sum[:], signature)
}

func random(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func marshal(m proto.Message) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(m)
}

// TokenKey is the key of the enrollment proof for a join token secret: the hash the server
// stores for the token (auth.HashToken). The agent computes it and never sends it.
func TokenKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return []byte(hex.EncodeToString(sum[:]))
}

// TokenLookup identifies the join token of a key without revealing the key.
func TokenLookup(key []byte) []byte {
	sum := sha256.Sum256(key)
	return sum[:]
}

// CAPublicKey returns the key of the CA certificate, which signs server messages.
func CAPublicKey(ca *x509.Certificate) (*ecdsa.PublicKey, error) {
	pub, ok := ca.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, errors.New("the server CA has no ECDSA P-256 key")
	}
	return pub, nil
}

// Enrollment is an enrollment request of an agent and the X25519 key the server encrypts the
// node secret to.
type Enrollment struct {
	Request *agentv1.EnrollRequest
	key     *ecdh.PrivateKey
}

// NewEnrollment creates an enrollment request that proves the join token of key.
func NewEnrollment(key []byte, hostname, agentVersion string) (*Enrollment, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	req := &agentv1.EnrollRequest{Hostname: hostname, AgentVersion: agentVersion, Proof: &agentv1.EnrollProof{
		TokenLookup: TokenLookup(key), PublicKey: priv.PublicKey().Bytes(), Nonce: random(NonceSize)}}
	req.Proof.Mac = enrollMAC(key, req)
	return &Enrollment{Request: req, key: priv}, nil
}

func enrollMAC(key []byte, req *agentv1.EnrollRequest) []byte {
	p := req.GetProof()
	return mac(key, input(domainEnroll, p.GetTokenLookup(), p.GetPublicKey(), p.GetNonce(),
		[]byte(req.GetHostname()), []byte(req.GetAgentVersion())))
}

// CheckProof checks the form of the enrollment proof of a request.
func CheckProof(req *agentv1.EnrollRequest) error {
	p := req.GetProof()
	switch {
	case p == nil:
		return errors.New("the request has no enrollment proof")
	case len(p.GetTokenLookup()) != sha256.Size, len(p.GetPublicKey()) != 32, len(p.GetNonce()) != NonceSize,
		len(p.GetMac()) != sha256.Size:
		return errors.New("malformed enrollment proof")
	}
	return nil
}

// VerifyProof checks an enrollment request against the key of a join token.
func VerifyProof(key []byte, req *agentv1.EnrollRequest) error {
	if err := CheckProof(req); err != nil {
		return err
	}
	p := req.GetProof()
	if !hmac.Equal(p.GetTokenLookup(), TokenLookup(key)) || !hmac.Equal(p.GetMac(), enrollMAC(key, req)) {
		return errors.New("the enrollment proof does not match the join token")
	}
	return nil
}

// SealEnrollment completes the response to a verified enrollment request: it encrypts the node
// secret to the public key of the proof and signs the response with the CA key.
func SealEnrollment(caKey *ecdsa.PrivateKey, req *agentv1.EnrollRequest, resp *agentv1.EnrollResponse, secret []byte) error {
	peer, err := ecdh.X25519().NewPublicKey(req.GetProof().GetPublicKey())
	if err != nil {
		return fmt.Errorf("agent public key: %w", err)
	}
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	shared, err := priv.ECDH(peer)
	if err != nil {
		return fmt.Errorf("agent public key: %w", err)
	}
	aead, err := secretAEAD(shared)
	if err != nil {
		return err
	}
	nonce := random(aead.NonceSize())
	resp.NodeSecret = &agentv1.EncryptedSecret{PublicKey: priv.PublicKey().Bytes(), Nonce: nonce,
		Ciphertext: aead.Seal(nil, nonce, secret, []byte(resp.GetNodeId()))}
	resp.Signature, err = sign(caKey, enrollResponseInput(req.GetProof().GetMac(), resp))
	return err
}

func secretAEAD(shared []byte) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, shared, nil, hkdfInfo, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func enrollResponseInput(requestMAC []byte, resp *agentv1.EnrollResponse) []byte {
	s := resp.GetNodeSecret()
	return input(domainEnrollResponse, requestMAC, []byte(resp.GetNodeId()), []byte(resp.GetNodeName()),
		resp.GetCaCertDer(), s.GetPublicKey(), s.GetNonce(), s.GetCiphertext())
}

// Open verifies the response to the enrollment against the CA fingerprint of the join token and
// returns the CA certificate and the node secret.
func (e *Enrollment) Open(resp *agentv1.EnrollResponse, caFingerprint string) (*x509.Certificate, []byte, error) {
	sum := sha256.Sum256(resp.GetCaCertDer())
	if hex.EncodeToString(sum[:]) != strings.ToLower(caFingerprint) {
		return nil, nil, errors.New("server CA does not match the fingerprint in the join token")
	}
	ca, err := x509.ParseCertificate(resp.GetCaCertDer())
	if err != nil {
		return nil, nil, fmt.Errorf("parsing the server CA: %w", err)
	}
	pub, err := CAPublicKey(ca)
	if err != nil {
		return nil, nil, err
	}
	if !verify(pub, enrollResponseInput(e.Request.GetProof().GetMac(), resp), resp.GetSignature()) {
		return nil, nil, errors.New("invalid signature on the enrollment response")
	}
	secret, err := openSecret(e.key, resp.GetNodeId(), resp.GetNodeSecret())
	if err != nil {
		return nil, nil, err
	}
	return ca, secret, nil
}

func openSecret(key *ecdh.PrivateKey, nodeID string, s *agentv1.EncryptedSecret) ([]byte, error) {
	peer, err := ecdh.X25519().NewPublicKey(s.GetPublicKey())
	if err != nil {
		return nil, fmt.Errorf("server public key: %w", err)
	}
	shared, err := key.ECDH(peer)
	if err != nil {
		return nil, fmt.Errorf("server public key: %w", err)
	}
	aead, err := secretAEAD(shared)
	if err != nil {
		return nil, err
	}
	if len(s.GetNonce()) != aead.NonceSize() {
		return nil, errors.New("malformed node secret")
	}
	secret, err := aead.Open(nil, s.GetNonce(), s.GetCiphertext(), []byte(nodeID))
	if err != nil || len(secret) != SecretSize {
		return nil, errors.New("the node secret cannot be decrypted")
	}
	return secret, nil
}

// NewHello opens a session of a node with a fresh nonce.
func NewHello(nodeID string, secret []byte, now time.Time) *agentv1.SessionHello {
	h := &agentv1.SessionHello{NodeId: nodeID, AgentNonce: random(NonceSize), TimestampMs: now.UnixMilli()}
	h.Mac = mac(secret, helloInput(h))
	return h
}

func helloInput(h *agentv1.SessionHello) []byte {
	return input(domainHello, []byte(h.GetNodeId()), h.GetAgentNonce(), u64(uint64(h.GetTimestampMs()))) //nolint:gosec // the bits of the timestamp, not its value
}

func sessionInput(nodeID string, agentNonce, serverNonce []byte) []byte {
	return input(domainSession, []byte(nodeID), agentNonce, serverNonce)
}

// binding ties the messages of a session to the node and to the nonces of both sides.
type binding struct {
	nodeID                  string
	agentNonce, serverNonce []byte
}

func (b binding) input(domain string, seq uint64, body []byte) []byte {
	return input(domain, []byte(b.nodeID), b.agentNonce, b.serverNonce, u64(seq), body)
}

// ServerSession is the server side of a session: it signs the messages for the agent and
// verifies the messages of the agent. Seal and Open may run concurrently, but neither of them
// concurrently with itself.
type ServerSession struct {
	binding
	key      *ecdsa.PrivateKey
	secret   []byte
	sent     uint64
	received uint64
}

// Accept verifies a session hello with the node secret and returns the server side of the
// session and the answer to the hello.
func Accept(caKey *ecdsa.PrivateKey, secret []byte, hello *agentv1.SessionHello, now time.Time) (*ServerSession, *agentv1.SessionAccept, error) {
	if len(hello.GetAgentNonce()) != NonceSize {
		return nil, nil, errors.New("malformed session hello")
	}
	if !hmac.Equal(hello.GetMac(), mac(secret, helloInput(hello))) {
		return nil, nil, errors.New("invalid MAC on the session hello")
	}
	if skew := now.Sub(time.UnixMilli(hello.GetTimestampMs())).Abs(); skew > MaxClockSkew {
		return nil, nil, fmt.Errorf("the clock of the agent is %s off", skew.Round(time.Second))
	}
	b := binding{nodeID: hello.GetNodeId(), agentNonce: hello.GetAgentNonce(), serverNonce: random(NonceSize)}
	sig, err := sign(caKey, sessionInput(b.nodeID, b.agentNonce, b.serverNonce))
	if err != nil {
		return nil, nil, err
	}
	return &ServerSession{binding: b, key: caKey, secret: secret}, &agentv1.SessionAccept{ServerNonce: b.serverNonce, Signature: sig}, nil
}

// Seal signs a command message for the agent.
func (s *ServerSession) Seal(m *agentv1.ServerMessage) (*agentv1.ServerMessage, error) {
	if m.GetCommand() == nil {
		return nil, errors.New("only commands are sent in a session")
	}
	body, err := marshal(m)
	if err != nil {
		return nil, err
	}
	seq := s.sent + 1
	sig, err := sign(s.key, s.input(domainCommand, seq, body))
	if err != nil {
		return nil, err
	}
	s.sent = seq
	return &agentv1.ServerMessage{Body: &agentv1.ServerMessage_Signed{Signed: &agentv1.SignedMessage{
		Seq: seq, SignedBody: body, Signature: sig}}}, nil
}

// Open verifies a message of the agent and returns the message it carries.
func (s *ServerSession) Open(m *agentv1.AgentMessage) (*agentv1.AgentMessage, error) {
	env := m.GetSigned()
	if env == nil {
		return nil, errors.New("unsigned agent message")
	}
	if !hmac.Equal(env.GetMac(), mac(s.secret, s.input(domainAgent, env.GetSeq(), env.GetSignedBody()))) {
		return nil, errors.New("invalid MAC on an agent message")
	}
	if env.GetSeq() != s.received+1 {
		return nil, fmt.Errorf("agent message %d arrived after message %d", env.GetSeq(), s.received)
	}
	inner := &agentv1.AgentMessage{}
	if err := proto.Unmarshal(env.GetSignedBody(), inner); err != nil {
		return nil, fmt.Errorf("decoding an agent message: %w", err)
	}
	switch inner.GetBody().(type) {
	case *agentv1.AgentMessage_Hello, *agentv1.AgentMessage_Result, *agentv1.AgentMessage_Status:
	default:
		return nil, errors.New("unexpected agent message")
	}
	s.received = env.GetSeq()
	return inner, nil
}

// AgentSession is the agent side of a session: it authenticates the messages for the server
// and verifies the commands of the server. Seal and Open may run concurrently, but neither of
// them concurrently with itself.
type AgentSession struct {
	binding
	ca       *ecdsa.PublicKey
	secret   []byte
	sent     uint64
	received uint64
}

// Join verifies the answer of the server to a session hello and returns the agent side of the
// session.
func Join(ca *ecdsa.PublicKey, secret []byte, hello *agentv1.SessionHello, accept *agentv1.SessionAccept) (*AgentSession, error) {
	if accept == nil {
		return nil, errors.New("the server did not accept the session")
	}
	if len(accept.GetServerNonce()) != NonceSize {
		return nil, errors.New("malformed session answer")
	}
	b := binding{nodeID: hello.GetNodeId(), agentNonce: hello.GetAgentNonce(), serverNonce: accept.GetServerNonce()}
	if !verify(ca, sessionInput(b.nodeID, b.agentNonce, b.serverNonce), accept.GetSignature()) {
		return nil, errors.New("invalid signature on the session answer")
	}
	return &AgentSession{binding: b, ca: ca, secret: secret}, nil
}

// Seal authenticates a message for the server.
func (a *AgentSession) Seal(m *agentv1.AgentMessage) (*agentv1.AgentMessage, error) {
	switch m.GetBody().(type) {
	case *agentv1.AgentMessage_Hello, *agentv1.AgentMessage_Result, *agentv1.AgentMessage_Status:
	default:
		return nil, errors.New("only hello, results and status are sent in a session")
	}
	body, err := marshal(m)
	if err != nil {
		return nil, err
	}
	a.sent++
	return &agentv1.AgentMessage{Body: &agentv1.AgentMessage_Signed{Signed: &agentv1.SignedMessage{
		Seq: a.sent, SignedBody: body, Mac: mac(a.secret, a.input(domainAgent, a.sent, body))}}}, nil
}

// Open verifies a message of the server and returns the command it carries.
func (a *AgentSession) Open(m *agentv1.ServerMessage) (*agentv1.Command, error) {
	env := m.GetSigned()
	if env == nil {
		return nil, errors.New("unsigned server message")
	}
	if !verify(a.ca, a.input(domainCommand, env.GetSeq(), env.GetSignedBody()), env.GetSignature()) {
		return nil, errors.New("invalid signature on a server message")
	}
	if env.GetSeq() != a.received+1 {
		return nil, fmt.Errorf("server message %d arrived after message %d", env.GetSeq(), a.received)
	}
	inner := &agentv1.ServerMessage{}
	if err := proto.Unmarshal(env.GetSignedBody(), inner); err != nil {
		return nil, fmt.Errorf("decoding a server message: %w", err)
	}
	cmd := inner.GetCommand()
	if cmd == nil {
		return nil, errors.New("unexpected server message")
	}
	a.received = env.GetSeq()
	return cmd, nil
}
