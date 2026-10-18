// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package agentgw is the server side of the agent protocol (ADR 0009): an internal certificate
// authority, enrollment with join tokens and the mTLS command stream.
package agentgw

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
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/brokerium/brokerium/internal/secrets"
	"github.com/brokerium/brokerium/internal/store"
)

// CA signs server and agent certificates.
type CA struct {
	Cert *x509.Certificate
	Key  *ecdsa.PrivateKey
	DER  []byte
}

// Fingerprint is the hex SHA-256 of the CA certificate; join tokens carry it so agents can
// verify the server before they trust it.
func (ca *CA) Fingerprint() string {
	sum := sha256.Sum256(ca.DER)
	return hex.EncodeToString(sum[:])
}

// LoadOrCreateCA returns the stored CA or creates and stores a new one.
func LoadOrCreateCA(ctx context.Context, st *store.Store, box *secrets.Box) (*CA, error) {
	certPEM, sealed, err := st.GetCA(ctx)
	if err == nil {
		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			return nil, errors.New("stored CA certificate is not PEM")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		keyDER, err := box.Open(sealed)
		if err != nil {
			return nil, fmt.Errorf("decrypting CA key: %w", err)
		}
		key, err := x509.ParseECPrivateKey(keyDER)
		if err != nil {
			return nil, err
		}
		return &CA{Cert: cert, Key: key, DER: block.Bytes}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "Brokerium agent CA", Organization: []string{"Brokerium"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	sealedKey, err := box.Seal(keyDER)
	if err != nil {
		return nil, err
	}
	pemCert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	if err := st.SaveCA(ctx, pemCert, sealedKey); err != nil {
		return nil, err
	}
	// Another server may have stored a CA concurrently; reload to use the winner.
	return LoadOrCreateCA(ctx, st, box)
}

// ServerCertificate issues a TLS certificate for the gRPC endpoint with the chain attached.
func (ca *CA) ServerCertificate(names []string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: "brokerium-server"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	tmpl.IPAddresses = append(tmpl.IPAddresses, net.ParseIP("127.0.0.1"))
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.DER}, PrivateKey: key}, nil
}

// SignAgent signs an agent CSR; the certificate's common name is the node id.
func (ca *CA) SignAgent(csrDER []byte, nodeID string) ([]byte, string, error) {
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, "", fmt.Errorf("parsing CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, "", fmt.Errorf("CSR signature: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: nodeID, Organization: []string{"Brokerium agents"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, csr.PublicKey, ca.Key)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(der)
	return der, hex.EncodeToString(sum[:]), nil
}

// Pool returns a cert pool with the CA.
func (ca *CA) Pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.Cert)
	return p
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	return n
}
