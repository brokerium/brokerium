// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"testing"

	"github.com/brokerium/brokerium/internal/agent"
)

func TestTransportFlags(t *testing.T) {
	tests := []struct {
		name                       string
		transport, tlsMode, caFile string
		args                       []string
		want                       agent.Config
	}{
		{name: "defaults", want: agent.Config{Transport: agent.TransportMTLS, TLS: agent.TLSSystem}},
		{name: "environment", transport: "insecure", tlsMode: "skip-verify",
			want: agent.Config{Transport: agent.TransportInsecure, TLS: agent.TLSSkipVerify}},
		{name: "CA file from the environment", transport: "insecure", caFile: "/etc/ssl/proxy-ca.pem",
			want: agent.Config{Transport: agent.TransportInsecure, TLS: agent.TLSSystem, CAFile: "/etc/ssl/proxy-ca.pem"}},
		{name: "flags override the environment", transport: "insecure", tlsMode: "none",
			args: []string{"--transport", "mtls", "--tls", "system"},
			want: agent.Config{Transport: agent.TransportMTLS, TLS: agent.TLSSystem}},
		{name: "flags only", args: []string{"--transport=insecure", "--tls=none"},
			want: agent.Config{Transport: agent.TransportInsecure, TLS: agent.TLSNone}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BROKERIUM_AGENT_TRANSPORT", tt.transport)
			t.Setenv("BROKERIUM_AGENT_TLS", tt.tlsMode)
			t.Setenv("BROKERIUM_AGENT_CA_FILE", tt.caFile)
			fs, cfg := flags()
			if err := fs.Parse(tt.args); err != nil {
				t.Fatal(err)
			}
			if cfg.Transport != tt.want.Transport || cfg.TLS != tt.want.TLS || cfg.CAFile != tt.want.CAFile {
				t.Errorf("transport %q, tls %q, CA file %q; want %q, %q, %q", cfg.Transport, cfg.TLS, cfg.CAFile,
					tt.want.Transport, tt.want.TLS, tt.want.CAFile)
			}
		})
	}
}
