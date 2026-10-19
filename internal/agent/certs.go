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
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"

	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// The client certificate and its key live together in agent.pem, so replacing them is one
// rename. Agents enrolled before renewal existed keep agent.crt and agent.key until the first
// renewal.
const (
	credentialsFile = "agent.pem"
	legacyCertFile  = "agent.crt"
	legacyKeyFile   = "agent.key"
)

// credentialPaths returns the certificate and key files of the client certificate.
func (a *Agent) credentialPaths() (certFile, keyFile string) {
	if _, err := os.Stat(a.path(credentialsFile)); err == nil {
		return a.path(credentialsFile), a.path(credentialsFile)
	}
	return a.path(legacyCertFile), a.path(legacyKeyFile)
}

// certificateExpired reports whether the stored client certificate can no longer authenticate.
func (a *Agent) certificateExpired() bool {
	pair, err := tls.LoadX509KeyPair(a.credentialPaths())
	return err == nil && time.Now().After(pair.Leaf.NotAfter)
}

// saveCredentials stores a client certificate with its key.
func (a *Agent) saveCredentials(certDER []byte, key *ecdsa.PrivateKey) error {
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	data := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})...)
	if err := writeFileAtomic(a.path(credentialsFile), data, 0o600); err != nil {
		return err
	}
	for _, f := range []string{legacyCertFile, legacyKeyFile} {
		if err := os.Remove(a.path(f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// writeFileAtomic replaces path with data; a crash leaves either the old or the new content.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// renewalTime is when a certificate is replaced: after two thirds of its validity.
func renewalTime(c *x509.Certificate) time.Time {
	return c.NotBefore.Add(c.NotAfter.Sub(c.NotBefore) * 2 / 3)
}

// keepCertificateRenewed renews the client certificate over conn whenever two thirds of its
// validity passed, until ctx ends. The running connection and Kafka are not touched; the next
// connection uses the new certificate.
func (a *Agent) keepCertificateRenewed(ctx context.Context, conn *grpc.ClientConn, current *x509.Certificate, roots *x509.CertPool) {
	client := agentv1.NewAgentServiceClient(conn)
	wait := time.Until(renewalTime(current))
	retry := time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(max(wait, 0)):
		}
		renewed, err := a.renewCertificate(ctx, client, roots)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.log.Warn("renewing the agent certificate", "error", err, "retryIn", retry, "expires", current.NotAfter)
			wait, retry = retry, min(2*retry, time.Hour)
			continue
		}
		a.log.Info("agent certificate renewed", "expires", renewed.NotAfter)
		current, retry = renewed, time.Minute
		wait = time.Until(renewalTime(current))
	}
}

// renewCertificate requests a certificate for a new key and stores both.
func (a *Agent) renewCertificate(ctx context.Context, client agentv1.AgentServiceClient, roots *x509.CertPool) (*x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: a.node.NodeID}}, key)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := client.RenewCertificate(cctx, &agentv1.RenewCertificateRequest{CsrDer: csr})
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(resp.GetCertDer())
	if err != nil {
		return nil, fmt.Errorf("parsing the renewed certificate: %w", err)
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		return nil, errors.New("the renewed certificate is not for the new key")
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return nil, fmt.Errorf("verifying the renewed certificate: %w", err)
	}
	if err := a.saveCredentials(resp.GetCertDer(), key); err != nil {
		return nil, fmt.Errorf("storing the renewed certificate: %w", err)
	}
	return cert, nil
}
