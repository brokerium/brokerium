// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

// TestMultiClusterOverview is scenario 7 in read-only mode: several clusters in one overview,
// including one without agents (beta, provider external).
func TestMultiClusterOverview(t *testing.T) {
	c := newClient(t)
	var list struct {
		Clusters []struct {
			Cluster struct {
				Name     string `json:"name"`
				Provider string `json:"provider"`
			} `json:"cluster"`
			Health struct {
				Status  string `json:"status"`
				Brokers int    `json:"brokers"`
				Voters  int    `json:"voters"`
			} `json:"health"`
			Capabilities struct {
				ProvisionNodes bool `json:"provisionNodes"`
				Reassign       bool `json:"reassign"`
			} `json:"capabilities"`
		} `json:"clusters"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/overview", nil, &list)
	healthy, external := 0, false
	for _, s := range list.Clusters {
		t.Logf("%-10s %-9s %-12s brokers=%d voters=%d", s.Cluster.Name, s.Cluster.Provider, s.Health.Status, s.Health.Brokers, s.Health.Voters)
		if s.Health.Status == "ok" || s.Health.Status == "warning" {
			healthy++
			if s.Cluster.Provider == "external" {
				external = true
				if s.Capabilities.ProvisionNodes || !s.Capabilities.Reassign {
					t.Errorf("%s: an external cluster must offer reassignment but not node provisioning", s.Cluster.Name)
				}
			}
			if s.Health.Brokers == 0 || s.Health.Voters == 0 {
				t.Errorf("%s: health without brokers or voters", s.Cluster.Name)
			}
		}
	}
	if healthy < 2 {
		t.Fatalf("%d reachable clusters, want at least 2", healthy)
	}
	if !external {
		t.Error("no reachable cluster without agents")
	}
}
