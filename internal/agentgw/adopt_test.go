// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentgw

import "testing"

func TestAdoptedRole(t *testing.T) {
	t.Parallel()
	for roles, want := range map[string]string{"broker": "broker", "controller": "controller", " broker ": "broker",
		"broker,controller": "", "controller, broker": "", "": ""} {
		got, ok := AdoptedRole(roles)
		if got != want || ok != (want != "") {
			t.Errorf("AdoptedRole(%q) = %q, %v; want %q", roles, got, ok, want)
		}
	}
}
