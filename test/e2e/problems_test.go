// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e && enterprise

package e2e

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/edition"
)

type problem struct {
	Fingerprint string `json:"fingerprint"`
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Object      struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"object"`
	Fixes []struct {
		Operation string          `json:"operation"`
		Params    json.RawMessage `json:"params"`
	} `json:"fixes"`
	AcknowledgedBy string     `json:"acknowledgedBy"`
	MutedUntil     *time.Time `json:"mutedUntil"`
}

type problemCounts struct {
	ClusterID string `json:"clusterId"`
	Critical  int    `json:"critical"`
	Muted     int    `json:"muted"`
}

type auditEntry struct {
	Action string `json:"action"`
	Target string `json:"target"`
}

type problemReport struct {
	Problems []problem `json:"problems"`
	Muted    int       `json:"muted"`
}

// problems refreshes the view of a cluster and returns its problems.
func (c *client) problems(clusterID string, muted bool) problemReport {
	c.t.Helper()
	c.view(clusterID)
	path := "/api/v1/clusters/" + clusterID + "/problems"
	if muted {
		path += "?muted=true"
	}
	var rep problemReport
	c.must(http.StatusOK, "GET", path, nil, &rep)
	return rep
}

func (r problemReport) find(code, topic string) (problem, bool) {
	for _, p := range r.Problems {
		if p.Code == code && p.Object.Kind == "topic" && p.Object.ID == topic {
			return p, true
		}
	}
	return problem{}, false
}

// waitProblem waits until a problem of a topic appears or, with present false, goes away.
func (c *client) waitProblem(clusterID, code, topic string, present bool) problem {
	c.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		p, found := c.problems(clusterID, true).find(code, topic)
		if found == present {
			return p
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("problem %s of topic %s: present %v after 90 s, want %v", code, topic, found, present)
		}
		time.Sleep(2 * time.Second)
	}
}

// TestEnterpriseProblems covers the problems of Brokerium Enterprise on the lab cluster alpha:
// the server refuses them without a license; a topic with one replica and min.insync.replicas 2
// is a critical finding that can be acknowledged and muted, and its fix, change-replication-factor,
// resolves it; alter-topic-configs changes min.insync.replicas. Run it against a server built with
// -tags enterprise,licensetest:
//
//	go test -tags e2e,enterprise -run TestEnterpriseProblems ./test/e2e
func TestEnterpriseProblems(t *testing.T) {
	c := newClient(t)
	withoutLicense(t, c)
	alpha := c.clusterID("alpha")
	base := "/api/v1/clusters/" + alpha
	topic := uniqueName("e2e-problems-")
	fixParams := map[string]any{"topics": []string{topic}, "replicationFactor": 3}

	for _, r := range []struct{ method, path string }{{"GET", base + "/problems"}, {"GET", "/api/v1/problems"}} {
		if status, raw := c.send(r.method, r.path, nil); status != http.StatusForbidden || errorCode(raw) != "license_required" {
			t.Errorf("without a license: %s %s = %d %s, want 403 license_required", r.method, r.path, status, raw)
		}
	}
	if status, raw := c.send("POST", base+"/operations", map[string]any{"type": "change-replication-factor", "params": fixParams}); status != http.StatusForbidden ||
		errorCode(raw) != "license_required" {
		t.Errorf("planning change-replication-factor without a license: %d %s", status, raw)
	}
	if status, raw := c.send("PUT", "/api/v1/license", map[string]string{"key": signLicense(t, testSigningKey(t), edition.Problems)}); status != http.StatusOK {
		t.Fatalf("adding a key of the test key: %d %s; is the server built with -tags enterprise,licensetest?", status, raw)
	}
	t.Cleanup(func() { c.do("DELETE", "/api/v1/license", nil, nil) })

	c.must(http.StatusCreated, "POST", base+"/topics", map[string]any{"name": topic, "partitions": 3, "replicationFactor": 1,
		"configs": map[string]string{"min.insync.replicas": "2"}}, nil)
	t.Cleanup(func() { c.do("DELETE", base+"/topics/"+topic+"?confirm="+topic, nil, nil) })

	p := c.waitProblem(alpha, "topic-rf-below-min-isr", topic, true)
	if p.Severity != "critical" || len(p.Fixes) == 0 || p.Fixes[0].Operation != "change-replication-factor" {
		t.Fatalf("finding %+v", p)
	}
	var fix struct {
		Topics            []string `json:"topics"`
		ReplicationFactor int      `json:"replicationFactor"`
	}
	if err := json.Unmarshal(p.Fixes[0].Params, &fix); err != nil || !slices.Equal(fix.Topics, []string{topic}) || fix.ReplicationFactor != 3 {
		t.Fatalf("fix %s: %v", p.Fixes[0].Params, err)
	}
	path := base + "/problems/" + p.Fingerprint

	var state struct {
		AcknowledgedBy string     `json:"acknowledgedBy"`
		MutedUntil     *time.Time `json:"mutedUntil"`
	}
	c.must(http.StatusOK, "POST", path+"/acknowledge", nil, &state)
	if state.AcknowledgedBy != env("BROKERIUM_E2E_USER", "admin") {
		t.Errorf("acknowledged by %q", state.AcknowledgedBy)
	}
	if status := c.do("POST", path+"/mute", map[string]any{"until": time.Now().Add(-time.Minute)}, nil); status != http.StatusBadRequest {
		t.Errorf("muting until a past time: status %d, want 400", status)
	}
	c.must(http.StatusOK, "POST", path+"/mute", map[string]any{"until": time.Now().Add(time.Hour)}, &state)
	if rep := c.problems(alpha, false); rep.Muted == 0 {
		t.Error("no problem counts as muted")
	} else if _, found := rep.find(p.Code, topic); found {
		t.Error("the muted problem is listed without muted=true")
	}
	muted, found := c.problems(alpha, true).find(p.Code, topic)
	if !found || muted.MutedUntil == nil || muted.AcknowledgedBy == "" {
		t.Errorf("muted problem %+v (found %v)", muted, found)
	}
	var counts struct {
		Clusters []problemCounts `json:"clusters"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/problems", nil, &counts)
	if i := slices.IndexFunc(counts.Clusters, func(x problemCounts) bool { return x.ClusterID == alpha }); i < 0 ||
		counts.Clusters[i].Muted == 0 {
		t.Errorf("counts %+v", counts.Clusters)
	}
	c.must(http.StatusOK, "DELETE", path+"/mute", nil, &state)
	if _, found := c.problems(alpha, false).find(p.Code, topic); !found {
		t.Error("the unmuted problem is not listed")
	}

	op := c.run(alpha, "alpha", p.Fixes[0].Operation, json.RawMessage(p.Fixes[0].Params), 5*time.Minute)
	if !strings.Contains(op.Summary, "to 3") {
		t.Errorf("summary %q", op.Summary)
	}
	for _, tp := range c.view(alpha).Snapshot.Topics {
		if tp.Name != topic {
			continue
		}
		for _, part := range tp.Partitions {
			if len(part.Replicas) != 3 {
				t.Errorf("%s-%d has replicas %v after the fix", topic, part.ID, part.Replicas)
			}
		}
	}
	c.waitProblem(alpha, "topic-rf-below-min-isr", topic, false)

	c.run(alpha, "alpha", "alter-topic-configs", map[string]any{"topics": []string{topic},
		"set": map[string]string{"min.insync.replicas": "1"}}, time.Minute)
	c.waitProblem(alpha, "topic-min-isr-one", topic, true)

	var audit struct {
		Entries []auditEntry `json:"entries"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/audit?limit=50&clusterId="+alpha, nil, &audit)
	for _, action := range []string{"problem.acknowledge", "problem.mute", "problem.unmute"} {
		if !slices.ContainsFunc(audit.Entries, func(e auditEntry) bool { return e.Action == action && e.Target == p.Fingerprint }) {
			t.Errorf("the audit log has no %s of %s", action, p.Fingerprint)
		}
	}
}
