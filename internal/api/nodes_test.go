// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/store"
)

func TestInstallCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		api  NodesAPI
		want string
	}{
		{"direct", NodesAPI{PublicURL: "https://brokerium.example.com/", AgentEndpoint: "agents.example.com:8443"},
			"curl -fsSL https://brokerium.example.com/api/v1/agent/install.sh | sudo BROKERIUM_AGENT_SERVER=agents.example.com:8443 BROKERIUM_AGENT_TOKEN=brk1.t sh"},
		{"proxy", NodesAPI{PublicURL: "https://brokerium.example.com", AgentEndpoint: "agents.example.com:8443",
			Proxy: "http://proxy.corp:3128", NoProxy: "localhost,.corp"},
			"HTTPS_PROXY='http://proxy.corp:3128' NO_PROXY='localhost,.corp' curl -fsSL https://brokerium.example.com/api/v1/agent/install.sh | " +
				"sudo HTTPS_PROXY='http://proxy.corp:3128' NO_PROXY='localhost,.corp' BROKERIUM_AGENT_SERVER=agents.example.com:8443 BROKERIUM_AGENT_TOKEN=brk1.t sh"},
		{"quote in the proxy", NodesAPI{PublicURL: "https://b", AgentEndpoint: "a:1", Proxy: "http://u:p'w@proxy:3128"},
			`HTTPS_PROXY='http://u:p'\''w@proxy:3128' curl -fsSL https://b/api/v1/agent/install.sh | sudo HTTPS_PROXY='http://u:p'\''w@proxy:3128' BROKERIUM_AGENT_SERVER=a:1 BROKERIUM_AGENT_TOKEN=brk1.t sh`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.api.installCommand("brk1.t"); got != tt.want {
				t.Errorf("installCommand =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestInstallScriptSyntax(t *testing.T) {
	t.Parallel()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	path := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(path, []byte(installScript), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(sh, "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v: %s", err, out)
	}
}

func TestCertificateState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	tests := []struct {
		notAfter *time.Time
		want     string
	}{
		{nil, ""},
		{at(100 * 24 * time.Hour), "ok"},
		{at(30 * 24 * time.Hour), "expiring"},
		{at(time.Minute), "expiring"},
		{at(0), "expired"},
		{at(-time.Hour), "expired"},
	}
	for _, tt := range tests {
		if got := certificateState(&store.Node{CertNotAfter: tt.notAfter}, now, 30*24*time.Hour); got != tt.want {
			t.Errorf("certificateState(%v) = %q, want %q", tt.notAfter, got, tt.want)
		}
	}
}
