// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Command gencerts writes a throwaway CA, a broker certificate and a client certificate for
// the TLS listeners of the lab (PEM files, Kafka's PEM keystore format).
//
//	go run ./deploy/lab/gencerts -out deploy/lab/.certs
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func main() {
	out := flag.String("out", "deploy/lab/.certs", "output directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}

	caKey := newKey()
	caTmpl := &x509.Certificate{
		SerialNumber: serial(), Subject: pkix.Name{CommonName: "Brokerium lab CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(5, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		log.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	write(*out, "ca.pem", pemBlock("CERTIFICATE", caDER))

	leaf := func(cn string, usage x509.ExtKeyUsage, dns []string) ([]byte, []byte) {
		key := newKey()
		tmpl := &x509.Certificate{
			SerialNumber: serial(), Subject: pkix.Name{CommonName: cn}, DNSNames: dns,
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(2, 0, 0),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
		if err != nil {
			log.Fatal(err)
		}
		pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			log.Fatal(err)
		}
		return pemBlock("CERTIFICATE", der), pemBlock("PRIVATE KEY", pkcs8)
	}

	brokerCert, brokerKey := leaf("beta-b1", x509.ExtKeyUsageServerAuth, []string{"localhost", "beta-b1"})
	// Kafka's PEM keystore: unencrypted PKCS#8 key followed by the certificate chain.
	write(*out, "broker-keystore.pem", append(append(brokerKey, brokerCert...), pemBlock("CERTIFICATE", caDER)...))

	clientCert, clientKey := leaf("brokerium", x509.ExtKeyUsageClientAuth, nil)
	write(*out, "client.pem", clientCert)
	write(*out, "client-key.pem", clientKey)
	log.Printf("wrote ca.pem, broker-keystore.pem, client.pem and client-key.pem to %s", *out)
}

func newKey() *ecdsa.PrivateKey {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	return k
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		log.Fatal(err)
	}
	return n
}

func pemBlock(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

func write(dir, name string, data []byte) {
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		log.Fatal(err)
	}
}
