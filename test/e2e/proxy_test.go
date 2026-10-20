// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/brokerium/brokerium/internal/agentauth"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// proxiedHosts are the lab agents the environment enrolls through the proxy.
var proxiedHosts = []string{"proxied-c1", "proxied-b101"}

type proxiedNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Connected bool   `json:"connected"`
	Status    struct {
		Hostname      string `json:"hostname"`
		Running       bool   `json:"running"`
		ActiveVersion string `json:"activeVersion"`
	} `json:"status"`
}

func proxiedNodes(c *client) map[string]proxiedNode {
	c.t.Helper()
	var res struct {
		Nodes []proxiedNode `json:"nodes"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/nodes", nil, &res)
	out := map[string]proxiedNode{}
	for _, n := range res.Nodes {
		out[n.Name] = n
	}
	return out
}

// TestInsecureAgentBehindProxy covers the insecure agent transport behind a reverse proxy that
// terminates TLS (OpenResty with grpc_pass to BROKERIUM_AGENT_INSECURE_ADDR). It starts nothing
// itself: the environment runs the proxy and the lab agents proxied-c1 and proxied-b101,
// enrolled through it with BROKERIUM_AGENT_TRANSPORT=insecure, and the server resolves their
// names to their Kafka ports (9093 and 9092). The test enrolls through the proxy with its own
// join token, then creates a cluster on the two agents, which sends them signed commands.
//
//	BROKERIUM_E2E_INSECURE_PROXY=localhost:9443  the proxy, dialed with TLS without verification
func TestInsecureAgentBehindProxy(t *testing.T) {
	proxy := os.Getenv("BROKERIUM_E2E_INSECURE_PROXY")
	if proxy == "" {
		t.Skip("set BROKERIUM_E2E_INSECURE_PROXY to the proxy in front of the insecure agent listener")
	}
	c := newClient(t)
	checkEnrollmentProofs(t, c, proxy)

	deadline := time.Now().Add(3 * time.Minute)
	for {
		nodes, ready := proxiedNodes(c), 0
		for _, name := range proxiedHosts {
			if n := nodes[name]; n.Connected && n.Transport == "insecure" && n.Status.Hostname == name {
				ready++
			}
		}
		if ready == len(proxiedHosts) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d agents connected through the proxy and reported their status: %+v", ready, len(proxiedHosts), nodes)
		}
		time.Sleep(2 * time.Second)
	}

	// The host names are fixed, so an earlier registration is replaced.
	const name = "proxied"
	var list struct {
		Clusters []summary `json:"clusters"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/clusters", nil, &list)
	for _, s := range list.Clusters {
		if s.Cluster.Name == name {
			c.must(http.StatusNoContent, "DELETE", "/api/v1/clusters/"+s.Cluster.ID, nil, nil)
		}
	}
	var created struct {
		Cluster struct {
			ID string `json:"id"`
		} `json:"cluster"`
	}
	c.must(http.StatusCreated, "POST", "/api/v1/clusters", map[string]any{"name": name, "provider": "agent", "skipTest": true,
		"bootstrapServers": []string{}}, &created)
	id := created.Cluster.ID
	// Installing, configuring, formatting and starting Kafka are signed commands; every step
	// waits for the authenticated result.
	op := c.run(id, name, "bootstrap-cluster", map[string]any{"controllers": 1, "brokers": 1, "racks": []string{"r1"}}, 10*time.Minute)
	t.Logf("through the proxy: %s", op.Summary)
	waitView(t, c, id, 3*time.Minute, "a healthy cluster on the proxied agents", healthy)
	c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+id+"/topics", map[string]any{"name": "behind-the-proxy", "partitions": 1, "replicationFactor": 1}, nil)
	c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+id+"/topics/behind-the-proxy/messages", map[string]any{"value": "hello"}, nil)

	deadline = time.Now().Add(time.Minute)
	for {
		nodes, running := proxiedNodes(c), 0
		for _, host := range proxiedHosts {
			if n := nodes[host]; n.Connected && n.Status.Running && n.Status.ActiveVersion != "" {
				running++
			}
		}
		if running == len(proxiedHosts) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d agents report a running Kafka: %+v", running, len(proxiedHosts), nodes)
		}
		time.Sleep(2 * time.Second)
	}
}

// checkEnrollmentProofs enrolls through the proxy like an agent: a proof keyed by another token
// is refused and leaves the token valid, the proof of the token returns a node secret that
// opens with the CA of the token, and the same request is refused once the token is used.
func checkEnrollmentProofs(t *testing.T, c *client, proxy string) {
	t.Helper()
	nodeName := uniqueName("proxied-proof-")
	var tok struct {
		Token string `json:"token"`
	}
	c.must(http.StatusCreated, "POST", "/api/v1/join-tokens", map[string]any{"nodeName": nodeName, "ttlMinutes": 5}, &tok)
	parts := strings.Split(tok.Token, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed join token %q", tok.Token)
	}
	key := agentauth.TokenKey(parts[1])

	conn, err := grpc.NewClient(proxy, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	agents := agentv1.NewAgentServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	forged, err := agentauth.NewEnrollment(agentauth.TokenKey("not-the-token"), nodeName, "e2e")
	if err != nil {
		t.Fatal(err)
	}
	forged.Request.Proof.TokenLookup = agentauth.TokenLookup(key)
	if _, err := agents.Enroll(ctx, forged.Request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a proof keyed by another token: %v, want PermissionDenied", err)
	}
	e, err := agentauth.NewEnrollment(key, nodeName, "e2e")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := agents.Enroll(ctx, e.Request)
	if err != nil {
		t.Fatalf("enrolling through the proxy: %v", err)
	}
	t.Cleanup(func() { c.must(http.StatusNoContent, "DELETE", "/api/v1/nodes/"+resp.GetNodeId(), nil, nil) })
	if _, secret, err := e.Open(resp, parts[2]); err != nil || len(secret) != agentauth.SecretSize {
		t.Fatalf("opening the enrollment response: %v", err)
	}
	if _, err := agents.Enroll(ctx, e.Request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("the replayed enrollment: %v, want PermissionDenied", err)
	}
	if _, err := agents.RenewCertificate(ctx, &agentv1.RenewCertificateRequest{}); status.Code(err) != codes.Unimplemented {
		t.Errorf("RenewCertificate through the proxy: %v, want Unimplemented", err)
	}
	nodes := proxiedNodes(c)
	if n := nodes[nodeName]; n.Transport != "insecure" || n.Connected {
		t.Errorf("node %s: transport %q, connected %v; want insecure and not connected", nodeName, n.Transport, n.Connected)
	}
}
