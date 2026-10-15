// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestRollingUpgrade is scenario 8: upgrade every node one at a time from 4.3.1 to 4.4.0 and
// finalize metadata.version afterwards.
func TestRollingUpgrade(t *testing.T) {
	heavy(t)
	from, to := env("BROKERIUM_E2E_UPGRADE_FROM", "4.3.1"), env("BROKERIUM_E2E_UPGRADE_TO", "4.4.0")
	c := newClient(t)
	name := uniqueName("e2e-upgrade-")
	id := dockerCluster(t, c, name, map[string]any{"controllers": 3, "brokers": 3, "version": from})
	before := waitView(t, c, id, 2*time.Minute, "a healthy cluster", healthy)
	produce(t, c, id, "before-upgrade", 6, 60)
	t.Logf("metadata.version before: %s", before.Health.MetadataVersion)

	plan := c.plan(id, "rolling-upgrade", map[string]any{"version": to})
	warned := false
	for _, w := range plan.Plan.Warnings {
		t.Logf("plan warning: %s", w)
		warned = warned || strings.Contains(w, "KAFKA-20979")
	}
	if strings.HasPrefix(from, "4.3.") && from < "4.3.2" && !warned {
		t.Errorf("no KAFKA-20979 warning for an upgrade from %s", from)
	}
	c.must(http.StatusOK, "POST", "/api/v1/operations/"+plan.ID+"/confirm", map[string]any{}, nil)
	op := c.wait(plan.ID, 40*time.Minute)
	t.Logf("scenario 8: %s", op.Summary)

	v := waitView(t, c, id, 3*time.Minute, "a healthy cluster on the new version", healthy)
	var nodes struct {
		Nodes []struct {
			Name   string `json:"name"`
			Status struct {
				ActiveVersion string `json:"activeVersion"`
				JavaVersion   string `json:"javaVersion"`
			} `json:"status"`
		} `json:"nodes"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/nodes?clusterId="+id, nil, &nodes)
	for _, n := range nodes.Nodes {
		if n.Status.ActiveVersion != to {
			t.Errorf("%s runs %q, want %s", n.Name, n.Status.ActiveVersion, to)
		}
		if n.Status.JavaVersion == "" {
			t.Errorf("%s reports no Java version", n.Name)
		}
	}
	if v.Health.MetadataVersion != before.Health.MetadataVersion {
		t.Errorf("metadata.version changed before finalizing: %s -> %s", before.Health.MetadataVersion, v.Health.MetadataVersion)
	}

	c.run(id, name, "finalize-upgrade", map[string]any{}, 5*time.Minute)
	v = waitView(t, c, id, time.Minute, "metadata.version of "+to, func(v view) bool {
		return strings.HasPrefix(v.Health.MetadataVersion, to[:3])
	})
	t.Logf("metadata.version after finalizing: %s", v.Health.MetadataVersion)

	var browsed struct {
		Messages []any `json:"messages"`
	}
	total := 0
	for p := 0; p < 6; p++ {
		c.must(http.StatusOK, "GET", "/api/v1/clusters/"+id+"/topics/before-upgrade/messages?from=earliest&limit=100&partition="+string(rune('0'+p)), nil, &browsed)
		total += len(browsed.Messages)
	}
	if total != 60 {
		t.Errorf("read %d of the 60 records written before the upgrade", total)
	}
}
