// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

// TestLicenseCommunity checks the license API of a Community server: the catalog of Enterprise
// features without a license, and license keys are refused.
func TestLicenseCommunity(t *testing.T) {
	c := newClient(t)
	var v struct {
		Edition  string `json:"edition"`
		Features []struct {
			ID       string `json:"id"`
			Licensed bool   `json:"licensed"`
		} `json:"features"`
		License *struct {
			State string `json:"state"`
		} `json:"license"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/license", nil, &v)
	if v.Edition != "community" {
		t.Skipf("the server runs the %s edition", v.Edition)
	}
	if len(v.Features) == 0 || v.License != nil {
		t.Fatalf("GET /api/v1/license = %+v", v)
	}
	for _, f := range v.Features {
		if f.Licensed {
			t.Errorf("feature %s is licensed in the Community edition", f.ID)
		}
	}
	for _, method := range []string{"PUT", "DELETE"} {
		if got := c.do(method, "/api/v1/license", map[string]string{"key": "brk-lic-1.x.y"}, nil); got != http.StatusConflict {
			t.Errorf("%s /api/v1/license: status %d, want 409", method, got)
		}
	}
}
