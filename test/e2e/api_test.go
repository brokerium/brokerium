// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

// Package e2e runs the lab scenarios against a running brokerium-server and Docker lab.
//
// Environment:
//
//	BROKERIUM_E2E_URL       server URL (default http://localhost:8080)
//	BROKERIUM_E2E_USER      admin user (default admin)
//	BROKERIUM_E2E_PASSWORD  its password (default brokerium-admin)
//	BROKERIUM_E2E_HEAVY     set to 1 to run scenarios that create Docker clusters
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

type client struct {
	t     *testing.T
	base  string
	token string
}

// newClient logs in; tests are skipped when no server answers.
func newClient(t *testing.T) *client {
	t.Helper()
	c := &client{t: t, base: strings.TrimRight(env("BROKERIUM_E2E_URL", "http://localhost:8080"), "/")}
	resp, err := http.Get(c.base + "/healthz")
	if err != nil {
		t.Skipf("brokerium-server is not reachable at %s: %v", c.base, err)
	}
	resp.Body.Close()
	var login struct {
		Token string `json:"token"`
	}
	c.must(http.StatusOK, "POST", "/api/v1/auth/login", map[string]string{
		"username": env("BROKERIUM_E2E_USER", "admin"), "password": env("BROKERIUM_E2E_PASSWORD", "brokerium-admin")}, &login)
	c.token = login.Token
	return c
}

// send sends a request and returns the status code and the response body.
func (c *client) send(method, path string, body any) (int, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Brokerium-Request", "1")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// do sends a request and decodes a JSON response into out; it returns the status code.
func (c *client) do(method, path string, body, out any) int {
	c.t.Helper()
	status, raw := c.send(method, path, body)
	if out != nil && len(raw) > 0 && status < 300 {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: decoding %s: %v", method, path, raw, err)
		}
	}
	if status >= 300 {
		c.t.Logf("%s %s -> %d %s", method, path, status, raw)
	}
	return status
}

func (c *client) must(status int, method, path string, body, out any) {
	c.t.Helper()
	if got := c.do(method, path, body, out); got != status {
		c.t.Fatalf("%s %s: status %d, want %d", method, path, got, status)
	}
}

type summary struct {
	Cluster struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"cluster"`
	Health struct {
		Status string `json:"status"`
	} `json:"health"`
}

// clusterID finds a registered cluster by name.
func (c *client) clusterID(name string) string {
	c.t.Helper()
	var list struct {
		Clusters []summary `json:"clusters"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/clusters", nil, &list)
	for _, s := range list.Clusters {
		if s.Cluster.Name == name {
			return s.Cluster.ID
		}
	}
	c.t.Skipf("cluster %q is not registered", name)
	return ""
}

type broker struct {
	ID       int32  `json:"id"`
	Rack     string `json:"rack"`
	Alive    bool   `json:"alive"`
	Fenced   bool   `json:"fenced"`
	Replicas int    `json:"replicas"`
	Leaders  int    `json:"leaders"`
}

type view struct {
	Health struct {
		Status          string `json:"status"`
		UnderReplicated int    `json:"underReplicated"`
		Voters          int    `json:"voters"`
		QuorumLeader    int32  `json:"quorumLeader"`
		KRaftVersion    int    `json:"kraftVersion"`
		MetadataVersion string `json:"metadataVersion"`
	} `json:"health"`
	Snapshot struct {
		Brokers []broker `json:"brokers"`
		Quorum  struct {
			Voters []struct {
				NodeID int32 `json:"nodeId"`
			} `json:"voters"`
		} `json:"quorum"`
		Topics []struct {
			Name       string `json:"name"`
			Partitions []struct {
				ID       int32   `json:"id"`
				Leader   int32   `json:"leader"`
				Replicas []int32 `json:"replicas"`
			} `json:"partitions"`
		} `json:"topics"`
	} `json:"snapshot"`
}

func (c *client) view(id string) view {
	c.t.Helper()
	var v view
	c.must(http.StatusOK, "GET", "/api/v1/clusters/"+id+"?fresh=1", nil, &v)
	return v
}

type operation struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Error   string `json:"error"`
	Plan    struct {
		Irreversible bool     `json:"irreversible"`
		Warnings     []string `json:"warnings"`
		Moves        []struct {
			ReplicaMoves int `json:"replicaMoves"`
		} `json:"moves"`
	} `json:"plan"`
	Steps []struct {
		Name   string `json:"name"`
		Kind   string `json:"kind"`
		Status string `json:"status"`
		Error  string `json:"error"`
	} `json:"steps"`
}

// plan creates a planned operation.
func (c *client) plan(clusterID, typ string, params any) operation {
	c.t.Helper()
	var op operation
	c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+clusterID+"/operations", map[string]any{"type": typ, "params": params}, &op)
	if op.Status != "planned" {
		c.t.Fatalf("operation %s is %s, want planned", op.ID, op.Status)
	}
	return op
}

// run plans, confirms and waits for an operation; it fails the test unless it succeeds.
func (c *client) run(clusterID, clusterName, typ string, params any, timeout time.Duration) operation {
	c.t.Helper()
	op := c.plan(clusterID, typ, params)
	c.t.Logf("%s: %s", typ, op.Summary)
	confirm := map[string]string{}
	if op.Plan.Irreversible {
		confirm["confirmName"] = clusterName
	}
	c.must(http.StatusOK, "POST", "/api/v1/operations/"+op.ID+"/confirm", confirm, &op)
	return c.wait(op.ID, timeout)
}

func (c *client) wait(id string, timeout time.Duration) operation {
	c.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var op operation
		c.must(http.StatusOK, "GET", "/api/v1/operations/"+id, nil, &op)
		switch op.Status {
		case "succeeded":
			return op
		case "failed", "cancelled", "paused":
			for _, s := range op.Steps {
				if s.Error != "" {
					c.t.Logf("step %q (%s): %s", s.Name, s.Kind, s.Error)
				}
			}
			c.t.Fatalf("operation %s %s: %s", op.Type, op.Status, op.Error)
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("operation %s still %s after %s", op.Type, op.Status, timeout)
		}
		time.Sleep(2 * time.Second)
	}
}

func heavy(t *testing.T) {
	t.Helper()
	if os.Getenv("BROKERIUM_E2E_HEAVY") != "1" {
		t.Skip("set BROKERIUM_E2E_HEAVY=1 to create Docker clusters")
	}
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().Unix()%100000)
}
