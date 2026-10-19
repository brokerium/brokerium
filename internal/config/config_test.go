// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package config

import "testing"

func TestDefaultAgentImage(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"v0.2.0":              "bvaliev/brokerium-agent:0.2.0-lab",
		"v1.10.3":             "bvaliev/brokerium-agent:1.10.3-lab",
		"v0.2.0-12-gefa7a1b":  "bvaliev/brokerium-agent:lab",
		"v0.2.0-dirty":        "bvaliev/brokerium-agent:lab",
		"edge-efa7a1b0c4d2e1": "bvaliev/brokerium-agent:lab",
		"dev":                 "bvaliev/brokerium-agent:lab",
	}
	for v, want := range tests {
		if got := defaultAgentImage(v); got != want {
			t.Errorf("defaultAgentImage(%q) = %q, want %q", v, got, want)
		}
	}
}
