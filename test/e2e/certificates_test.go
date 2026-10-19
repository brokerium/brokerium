// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"net/http"
	"os"
	"testing"
	"time"
)

type agentNode struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Connected        bool       `json:"connected"`
	CertNotAfter     *time.Time `json:"certNotAfter"`
	CertificateState string     `json:"certificateState"`
	Status           struct {
		Running bool  `json:"running"`
		Pid     int64 `json:"pid"`
	} `json:"status"`
}

// TestAgentCertificateRenewal waits until an agent renews its certificate and checks that the
// node stays connected and Kafka keeps running in the same process. The server runs with a short
// certificate lifetime:
//
//	BROKERIUM_AGENT_CERT_LIFETIME=3m (server) BROKERIUM_E2E_CERT_LIFETIME=3m
func TestAgentCertificateRenewal(t *testing.T) {
	lifetime, err := time.ParseDuration(os.Getenv("BROKERIUM_E2E_CERT_LIFETIME"))
	if err != nil {
		t.Skip("BROKERIUM_E2E_CERT_LIFETIME is not set")
	}
	c := newClient(t)
	nodes := func() map[string]agentNode {
		var res struct {
			Nodes []agentNode `json:"nodes"`
		}
		c.must(http.StatusOK, "GET", "/api/v1/nodes", nil, &res)
		out := map[string]agentNode{}
		for _, n := range res.Nodes {
			out[n.ID] = n
		}
		return out
	}
	var before agentNode
	for _, n := range nodes() {
		if n.Connected && n.Status.Running && n.Status.Pid > 0 && n.CertNotAfter != nil {
			before = n
			break
		}
	}
	if before.ID == "" {
		t.Skip("no connected agent node runs Kafka")
	}
	deadline := time.Now().Add(lifetime)
	for {
		n := nodes()[before.ID]
		if n.CertNotAfter != nil && n.CertNotAfter.After(*before.CertNotAfter) {
			if !n.Connected || n.Status.Pid != before.Status.Pid || n.CertificateState != "ok" {
				t.Fatalf("after the renewal %s is connected=%v, Kafka pid %d (was %d), certificate %q",
					n.Name, n.Connected, n.Status.Pid, before.Status.Pid, n.CertificateState)
			}
			t.Logf("%s renewed its certificate (%s -> %s), Kafka pid %d", n.Name, before.CertNotAfter, n.CertNotAfter, n.Status.Pid)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not renew its certificate within %s; it expires at %s", before.Name, lifetime, before.CertNotAfter)
		}
		time.Sleep(5 * time.Second)
	}
}
