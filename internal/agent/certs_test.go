// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log/slog"
	"math/big"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"

	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return &testCA{cert: cert, key: key}
}

func (ca *testCA) sign(t *testing.T, pub any) []byte {
	t.Helper()
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "node-1"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, pub, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func (ca *testCA) pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.cert)
	return p
}

// renewClient answers RenewCertificate like the server, or with a certificate for another key.
type renewClient struct {
	agentv1.AgentServiceClient
	t        *testing.T
	ca       *testCA
	wrongKey bool
}

func (c *renewClient) RenewCertificate(_ context.Context, req *agentv1.RenewCertificateRequest, _ ...grpc.CallOption) (*agentv1.RenewCertificateResponse, error) {
	csr, err := x509.ParseCertificateRequest(req.GetCsrDer())
	if err != nil {
		return nil, err
	}
	pub := csr.PublicKey
	if c.wrongKey {
		other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		pub = &other.PublicKey
	}
	return &agentv1.RenewCertificateResponse{CertDer: c.ca.sign(c.t, pub)}, nil
}

func newTestAgent(t *testing.T) *Agent {
	t.Helper()
	return &Agent{cfg: Config{DataDir: t.TempDir()}, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		node: identity{NodeID: "node-1", NodeName: "alpha-b101"}}
}

func TestRenewCertificate(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	a := newTestAgent(t)
	// An agent enrolled before renewal existed has agent.crt and agent.key.
	for _, f := range []string{legacyCertFile, legacyKeyFile} {
		if err := os.WriteFile(a.path(f), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cert, err := a.renewCertificate(context.Background(), &renewClient{t: t, ca: ca}, ca.pool())
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := a.credentialPaths()
	if certFile != a.path(credentialsFile) || keyFile != certFile {
		t.Fatalf("credentials at %s and %s, want %s", certFile, keyFile, credentialsFile)
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if !pair.Leaf.Equal(cert) {
		t.Error("the stored certificate differs from the renewed one")
	}
	if info, _ := os.Stat(certFile); info.Mode().Perm() != 0o600 {
		t.Errorf("credentials have mode %v, want 0600", info.Mode().Perm())
	}
	for _, f := range []string{legacyCertFile, legacyKeyFile} {
		if _, err := os.Stat(a.path(f)); !os.IsNotExist(err) {
			t.Errorf("%s must be removed after the renewal", f)
		}
	}
}

func TestRenewCertificateRejectsForeignKey(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	a := newTestAgent(t)
	if _, err := a.renewCertificate(context.Background(), &renewClient{t: t, ca: ca, wrongKey: true}, ca.pool()); err == nil {
		t.Fatal("a certificate for another key must be rejected")
	}
	if _, err := a.renewCertificate(context.Background(), &renewClient{t: t, ca: newTestCA(t)}, ca.pool()); err == nil {
		t.Fatal("a certificate from another CA must be rejected")
	}
	if _, err := os.Stat(a.path(credentialsFile)); !os.IsNotExist(err) {
		t.Error("rejected certificates must not be stored")
	}
}

func TestRenewalTime(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	c := &x509.Certificate{NotBefore: start, NotAfter: start.Add(300 * 24 * time.Hour)}
	if got, want := renewalTime(c), start.Add(200*24*time.Hour); !got.Equal(want) {
		t.Errorf("renewalTime = %s, want %s", got, want)
	}
}
