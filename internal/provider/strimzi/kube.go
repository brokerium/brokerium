// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package strimzi manages Kafka clusters run by the Strimzi operator on Kubernetes through the
// Kubernetes API: node pools, versions and rolling restarts. Kafka-level work (reassignments,
// health) stays with the Kafka protocol.
package strimzi

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrNotFound is returned for missing Kubernetes objects.
var ErrNotFound = errors.New("not found")

// Kube is a minimal Kubernetes REST client.
type Kube struct {
	server string
	token  string
	http   *http.Client
}

const inClusterDir = "/var/run/secrets/kubernetes.io/serviceaccount"

// InCluster builds a client from the pod's service account.
func InCluster() (*Kube, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, errors.New("not running inside Kubernetes (KUBERNETES_SERVICE_HOST is not set)")
	}
	token, err := os.ReadFile(inClusterDir + "/token")
	if err != nil {
		return nil, fmt.Errorf("reading the service account token: %w", err)
	}
	ca, err := os.ReadFile(inClusterDir + "/ca.crt")
	if err != nil {
		return nil, fmt.Errorf("reading the cluster CA: %w", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca)
	return newKube("https://"+host+":"+port, strings.TrimSpace(string(token)), &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}), nil
}

type kubeconfig struct {
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
			InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			Token                 string `yaml:"token"`
			ClientCertificateData string `yaml:"client-certificate-data"`
			ClientKeyData         string `yaml:"client-key-data"`
		} `yaml:"user"`
	} `yaml:"users"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
}

// FromKubeconfig builds a client from kubeconfig content. Only embedded credentials are
// supported (token or client certificate data); exec plugins and file references are not,
// because the server keeps the kubeconfig sealed in its database.
func FromKubeconfig(content []byte, contextName string) (*Kube, error) {
	var kc kubeconfig
	if err := yaml.Unmarshal(content, &kc); err != nil {
		return nil, fmt.Errorf("parsing kubeconfig: %w", err)
	}
	if contextName == "" {
		contextName = kc.CurrentContext
	}
	var clusterName, userName string
	for _, c := range kc.Contexts {
		if c.Name == contextName {
			clusterName, userName = c.Context.Cluster, c.Context.User
		}
	}
	if clusterName == "" {
		return nil, fmt.Errorf("kubeconfig has no context %q", contextName)
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	server := ""
	for _, c := range kc.Clusters {
		if c.Name != clusterName {
			continue
		}
		server = c.Cluster.Server
		cfg.InsecureSkipVerify = c.Cluster.InsecureSkipTLSVerify //nolint:gosec // explicit kubeconfig choice
		if c.Cluster.CertificateAuthorityData != "" {
			ca, err := base64.StdEncoding.DecodeString(c.Cluster.CertificateAuthorityData)
			if err != nil {
				return nil, fmt.Errorf("decoding certificate-authority-data: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(ca) {
				return nil, errors.New("invalid certificate-authority-data")
			}
			cfg.RootCAs = pool
		}
	}
	if server == "" {
		return nil, fmt.Errorf("kubeconfig has no cluster %q", clusterName)
	}
	token := ""
	for _, u := range kc.Users {
		if u.Name != userName {
			continue
		}
		token = u.User.Token
		if u.User.ClientCertificateData != "" {
			certPEM, err1 := base64.StdEncoding.DecodeString(u.User.ClientCertificateData)
			keyPEM, err2 := base64.StdEncoding.DecodeString(u.User.ClientKeyData)
			if err := errors.Join(err1, err2); err != nil {
				return nil, fmt.Errorf("decoding the client certificate: %w", err)
			}
			cert, err := tls.X509KeyPair(certPEM, keyPEM)
			if err != nil {
				return nil, fmt.Errorf("client certificate: %w", err)
			}
			cfg.Certificates = []tls.Certificate{cert}
		}
	}
	if token == "" && len(cfg.Certificates) == 0 {
		return nil, fmt.Errorf("user %q has no token or client certificate data (exec plugins are not supported)", userName)
	}
	return newKube(server, token, cfg), nil
}

func newKube(server, token string, cfg *tls.Config) *Kube {
	return &Kube{server: strings.TrimRight(server, "/"), token: token,
		http: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg}}}
}

// Do sends a request; out receives the decoded JSON response.
func (k *Kube) Do(ctx context.Context, method, path, contentType string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.server+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if k.token != "" {
		req.Header.Set("Authorization", "Bearer "+k.token)
	}
	resp, err := k.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s: %w", path, ErrNotFound)
	case resp.StatusCode >= 300:
		var status struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &status)
		if status.Message == "" {
			status.Message = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, status.Message)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decoding %s: %w", path, err)
		}
	}
	return nil
}

// Get reads an object.
func (k *Kube) Get(ctx context.Context, path string, out any) error {
	return k.Do(ctx, http.MethodGet, path, "", nil, out)
}

// MergePatch applies a JSON merge patch (RFC 7386).
func (k *Kube) MergePatch(ctx context.Context, path string, patch, out any) error {
	return k.Do(ctx, http.MethodPatch, path, "application/merge-patch+json", patch, out)
}
