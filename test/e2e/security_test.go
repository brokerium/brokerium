// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type testResult struct {
	OK        bool   `json:"ok"`
	ClusterID string `json:"clusterId"`
	Brokers   int    `json:"brokers"`
	Error     string `json:"error"`
}

// TestSecureConnections registers the lab cluster beta through its SASL/SCRAM and mTLS
// listeners (phase 1: connection profiles).
func TestSecureConnections(t *testing.T) {
	c := newClient(t)
	beta := c.clusterID("beta")
	certs := filepath.Join("..", "..", "deploy", "lab", ".certs")
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join(certs, name))
		if err != nil {
			t.Skipf("lab certificates missing (make lab-certs): %v", err)
		}
		return string(raw)
	}
	ca, cert, key := read("ca.pem"), read("client.pem"), read("client-key.pem")

	c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+beta+"/scram-users",
		map[string]any{"user": "e2e-scram", "mechanism": "SCRAM-SHA-512", "password": "e2e-secret-1", "iterations": 8192}, nil)
	defer c.do("DELETE", "/api/v1/clusters/"+beta+"/scram-users/e2e-scram?mechanism=SCRAM-SHA-512", nil, nil)

	scram := func(password string) map[string]any {
		return map[string]any{"bootstrapServers": []string{"localhost:29201"},
			"security": map[string]any{"protocol": "SASL_PLAINTEXT", "saslMechanism": "SCRAM-SHA-512", "username": "e2e-scram"},
			"secrets":  map[string]any{"password": password}}
	}
	mtls := func(withCert bool) map[string]any {
		secrets := map[string]any{}
		if withCert {
			secrets = map[string]any{"tlsClientCertPem": cert, "tlsClientKeyPem": key}
		}
		return map[string]any{"bootstrapServers": []string{"localhost:29301"},
			"security": map[string]any{"protocol": "SSL", "tlsCaCertPem": ca}, "secrets": secrets}
	}
	tests := []struct {
		name    string
		profile map[string]any
		ok      bool
	}{
		{"SCRAM-SHA-512", scram("e2e-secret-1"), true},
		{"SCRAM with a wrong password", scram("wrong"), false},
		{"mTLS with a client certificate", mtls(true), true},
		{"TLS without a client certificate", mtls(false), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var res testResult
			c.must(http.StatusOK, "POST", "/api/v1/clusters/test", tc.profile, &res)
			if res.OK != tc.ok {
				t.Fatalf("ok = %v, want %v (error %q)", res.OK, tc.ok, res.Error)
			}
			if res.OK && (res.ClusterID == "" || res.Brokers != 1) {
				t.Errorf("cluster id %q, %d brokers", res.ClusterID, res.Brokers)
			}
		})
	}

	// The same Kafka cluster cannot be registered twice.
	dup := mtls(true)
	dup["name"] = uniqueName("beta-mtls-")
	if got := c.do("POST", "/api/v1/clusters", dup, nil); got != http.StatusConflict {
		t.Errorf("registering beta twice: status %d, want 409", got)
	}

	// Switch beta to mTLS (secrets are sealed and stored), read its health, switch back.
	c.must(http.StatusOK, "PATCH", "/api/v1/clusters/"+beta, mtls(true), nil)
	defer c.must(http.StatusOK, "PATCH", "/api/v1/clusters/"+beta, map[string]any{
		"bootstrapServers": []string{"localhost:29101"}, "security": map[string]any{"protocol": "PLAINTEXT"}, "secrets": map[string]any{}}, nil)
	if v := c.view(beta); v.Health.Status != "ok" || len(v.Snapshot.Brokers) != 1 {
		t.Errorf("beta over mTLS: health %s with %d brokers", v.Health.Status, len(v.Snapshot.Brokers))
	}
}
