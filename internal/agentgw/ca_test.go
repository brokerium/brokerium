// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentgw

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"
	"time"
)

func testCA(t *testing.T) *CA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &CA{Cert: cert, Key: key, DER: der}
}

func TestSignAgent(t *testing.T) {
	t.Parallel()
	ca := testCA(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "host"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, lifetime := range []time.Duration{3 * time.Minute, DefaultCertLifetime} {
		signed, err := ca.SignAgent(csr, "node-1", lifetime)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(signed.DER)
		if err != nil {
			t.Fatal(err)
		}
		if cert.Subject.CommonName != "node-1" || !cert.NotAfter.Equal(signed.NotAfter) {
			t.Errorf("certificate for %q until %s, want node-1 until %s", cert.Subject.CommonName, cert.NotAfter, signed.NotAfter)
		}
		if _, err := cert.Verify(x509.VerifyOptions{Roots: ca.Pool(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
			t.Errorf("verifying the agent certificate: %v", err)
		}
		if got := time.Until(cert.NotAfter); got < lifetime-time.Minute || got > lifetime {
			t.Errorf("lifetime %s: certificate valid for another %s", lifetime, got)
		}
		// Renewal lands at two thirds of the lifetime, whatever the clock skew allowance.
		if got := time.Until(RenewalTime(cert)); got < lifetime/2 || got > lifetime*2/3 {
			t.Errorf("lifetime %s: renewal in %s", lifetime, got)
		}
	}
}

func TestServingCertRotates(t *testing.T) {
	t.Parallel()
	ca := testCA(t)
	issued := 0
	lifetime := time.Hour
	s := &servingCert{issue: func() (tls.Certificate, error) {
		issued++
		return ca.ServerCertificate([]string{"brokerium"}, lifetime)
	}}
	first, err := s.get(nil)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := s.get(nil); again != first || issued != 1 {
		t.Fatalf("a fresh certificate must be reused, issued %d", issued)
	}
	first.Leaf.NotAfter = time.Now().Add(time.Minute)
	if rotated, _ := s.get(nil); rotated == first || issued != 2 {
		t.Fatalf("a certificate past two thirds of its lifetime must be replaced, issued %d", issued)
	}
}

func TestWarningPeriod(t *testing.T) {
	t.Parallel()
	tests := map[time.Duration]time.Duration{
		DefaultCertLifetime: 30 * 24 * time.Hour,
		90 * 24 * time.Hour: 15 * 24 * time.Hour,
		3 * time.Minute:     30 * time.Second,
	}
	for lifetime, want := range tests {
		if got := WarningPeriod(lifetime); got != want {
			t.Errorf("WarningPeriod(%s) = %s, want %s", lifetime, got, want)
		}
	}
}
