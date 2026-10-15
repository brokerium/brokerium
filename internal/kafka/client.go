// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package kafka wraps franz-go for Brokerium: client construction from connection profiles,
// KRaft-specific requests and cluster snapshots.
package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/aws"
	"github.com/twmb/franz-go/pkg/sasl/oauth"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/bvaliev/brokerium/internal/store"
	"github.com/bvaliev/brokerium/internal/version"
)

// Security protocols.
const (
	ProtocolPlaintext     = "PLAINTEXT"
	ProtocolSSL           = "SSL"
	ProtocolSASLPlaintext = "SASL_PLAINTEXT"
	ProtocolSASLSSL       = "SASL_SSL"
)

// Profile describes how to connect to a cluster.
type Profile struct {
	BootstrapServers []string
	Security         store.Security
	Secrets          store.Secrets
}

// Client bundles a franz-go client and its admin wrapper.
type Client struct {
	KGO *kgo.Client
	Adm *kadm.Client
}

// NewClient builds a client for a profile. Extra options are appended last.
func NewClient(p Profile, extra ...kgo.Opt) (*Client, error) {
	if len(p.BootstrapServers) == 0 {
		return nil, errors.New("no bootstrap servers")
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(p.BootstrapServers...),
		kgo.ClientID("brokerium"),
		kgo.SoftwareNameAndVersion("brokerium", strings.TrimPrefix(version.Version, "v")),
		kgo.RequestTimeoutOverhead(10 * time.Second),
		kgo.RetryTimeout(15 * time.Second),
		kgo.DialTimeout(5 * time.Second),
		kgo.MetadataMinAge(2 * time.Second),
		kgo.MetadataMaxAge(30 * time.Second),
	}
	protocol := strings.ToUpper(p.Security.Protocol)
	if protocol == "" {
		protocol = ProtocolPlaintext
	}
	if protocol == ProtocolSSL || protocol == ProtocolSASLSSL {
		cfg, err := tlsConfig(p)
		if err != nil {
			return nil, err
		}
		opts = append(opts, kgo.DialTLSConfig(cfg))
	}
	if protocol == ProtocolSASLPlaintext || protocol == ProtocolSASLSSL {
		mech, err := saslMechanism(p)
		if err != nil {
			return nil, err
		}
		opts = append(opts, kgo.SASL(mech))
	}
	opts = append(opts, extra...)
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &Client{KGO: cl, Adm: kadm.NewClient(cl)}, nil
}

// Close releases connections.
func (c *Client) Close() { c.KGO.Close() }

// Ping verifies that at least one broker answers.
func (c *Client) Ping(ctx context.Context) error {
	return c.KGO.Ping(ctx)
}

func tlsConfig(p Profile) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         p.Security.TLSServerName,
		InsecureSkipVerify: p.Security.TLSInsecureSkipVerify, //nolint:gosec // explicit user choice per cluster
	}
	if p.Security.TLSCACertPEM != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(p.Security.TLSCACertPEM)) {
			return nil, errors.New("invalid CA certificate PEM")
		}
		cfg.RootCAs = pool
	}
	if p.Secrets.TLSClientCertPEM != "" || p.Secrets.TLSClientKeyPEM != "" {
		cert, err := tls.X509KeyPair([]byte(p.Secrets.TLSClientCertPEM), []byte(p.Secrets.TLSClientKeyPEM))
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

func saslMechanism(p Profile) (sasl.Mechanism, error) {
	user, pass := p.Security.Username, p.Secrets.Password
	switch strings.ToUpper(p.Security.SASLMechanism) {
	case "PLAIN":
		return plain.Auth{User: user, Pass: pass}.AsMechanism(), nil
	case "SCRAM-SHA-256":
		return scram.Auth{User: user, Pass: pass}.AsSha256Mechanism(), nil
	case "SCRAM-SHA-512":
		return scram.Auth{User: user, Pass: pass}.AsSha512Mechanism(), nil
	case "AWS_MSK_IAM":
		auth := aws.Auth{
			AccessKey:    p.Secrets.AWSAccessKeyID,
			SecretKey:    p.Secrets.AWSSecretAccessKey,
			SessionToken: p.Secrets.AWSSessionToken,
			UserAgent:    "brokerium",
		}
		return aws.ManagedStreamingIAM(func(context.Context) (aws.Auth, error) { return auth, nil }), nil
	case "OAUTHBEARER":
		return oauth.Auth{Token: p.Secrets.OAuthToken}.AsMechanism(), nil
	default:
		return nil, fmt.Errorf("unsupported SASL mechanism %q", p.Security.SASLMechanism)
	}
}
