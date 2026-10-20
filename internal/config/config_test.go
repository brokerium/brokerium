// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestAgentTransportSettings(t *testing.T) {
	tests := []struct {
		name                                string
		addr, transport, endpoint, tlsMode  string
		wantTransport, wantTLS, wantErrPart string
	}{
		{name: "defaults", wantTransport: "mtls", wantTLS: "system"},
		{name: "listener for a proxy, install command with mutual TLS", addr: ":8444", wantTransport: "mtls", wantTLS: "system"},
		{name: "insecure install command", addr: ":8444", transport: "insecure", endpoint: "agents.example.com:443",
			tlsMode: "skip-verify", wantTransport: "insecure", wantTLS: "skip-verify"},
		{name: "plaintext to the proxy", addr: ":8444", transport: "insecure", endpoint: "10.0.0.5:80", tlsMode: "none",
			wantTransport: "insecure", wantTLS: "none"},
		{name: "unknown transport", transport: "tls", wantErrPart: "BROKERIUM_AGENT_TRANSPORT must be"},
		{name: "unknown TLS mode", addr: ":8444", tlsMode: "verify", wantErrPart: "BROKERIUM_AGENT_INSECURE_TLS must be"},
		{name: "insecure without the listener", transport: "insecure", endpoint: "agents.example.com:443",
			wantErrPart: "BROKERIUM_AGENT_INSECURE_ADDR"},
		{name: "insecure without an endpoint", addr: ":8444", transport: "insecure", wantErrPart: "needs BROKERIUM_AGENT_INSECURE_ENDPOINT"},
		{name: "endpoint without a port", addr: ":8444", transport: "insecure", endpoint: "agents.example.com",
			wantErrPart: "must be host:port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BROKERIUM_DATA_DIR", t.TempDir())
			t.Setenv("BROKERIUM_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
			t.Setenv("BROKERIUM_AGENT_INSECURE_ADDR", tt.addr)
			t.Setenv("BROKERIUM_AGENT_TRANSPORT", tt.transport)
			t.Setenv("BROKERIUM_AGENT_INSECURE_ENDPOINT", tt.endpoint)
			t.Setenv("BROKERIUM_AGENT_INSECURE_TLS", tt.tlsMode)
			c, err := LoadServer()
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("LoadServer error = %v, want one containing %q", err, tt.wantErrPart)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.AgentInsecureAddr != tt.addr || c.AgentTransport != tt.wantTransport || c.AgentInsecureEndpoint != tt.endpoint ||
				c.AgentInsecureTLS != tt.wantTLS {
				t.Errorf("got addr %q, transport %q, endpoint %q, TLS %q", c.AgentInsecureAddr, c.AgentTransport,
					c.AgentInsecureEndpoint, c.AgentInsecureTLS)
			}
		})
	}
}

func TestDockerProviderSetting(t *testing.T) {
	tests := []struct{ value, want, wantErrPart string }{
		{value: "", want: "auto"},
		{value: "auto", want: "auto"},
		{value: "on", want: "on"},
		{value: "off", want: "off"},
		{value: "true", wantErrPart: `BROKERIUM_DOCKER_PROVIDER must be auto, on or off, not "true"`},
		{value: "On", wantErrPart: "BROKERIUM_DOCKER_PROVIDER must be"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("BROKERIUM_DATA_DIR", t.TempDir())
			t.Setenv("BROKERIUM_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
			t.Setenv("BROKERIUM_DOCKER_PROVIDER", tt.value)
			c, err := LoadServer()
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("LoadServer error = %v, want one containing %q", err, tt.wantErrPart)
				}
				return
			}
			if err != nil || c.DockerProvider != tt.want {
				t.Fatalf("LoadServer = %q, %v, want %q", c.DockerProvider, err, tt.want)
			}
		})
	}
}

func TestAgentServerNames(t *testing.T) {
	tests := []struct {
		name, endpoint, dockerEndpoint, names string
		want                                  []string
	}{
		{name: "defaults", want: []string{"localhost", "host.docker.internal", "brokerium", "brokerium-server"}},
		{name: "endpoint of the Docker installation", dockerEndpoint: "brokerium:8443",
			want: []string{"localhost", "host.docker.internal", "brokerium", "brokerium-server"}},
		{name: "another container name", dockerEndpoint: "brokerium-verify:8443",
			want: []string{"localhost", "host.docker.internal", "brokerium", "brokerium-server", "brokerium-verify"}},
		{name: "hosts and Docker nodes", endpoint: "docker-host.example.com:8443", dockerEndpoint: "brokerium-verify:8443",
			want: []string{"localhost", "host.docker.internal", "brokerium", "brokerium-server", "docker-host.example.com", "brokerium-verify"}},
		{name: "explicit names", endpoint: "agents.example.com:443", names: "brokerium.internal",
			want: []string{"brokerium.internal", "agents.example.com"}},
		{name: "IP address", endpoint: "[2001:db8::1]:8443", names: "localhost", want: []string{"localhost", "2001:db8::1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BROKERIUM_DATA_DIR", t.TempDir())
			t.Setenv("BROKERIUM_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
			t.Setenv("BROKERIUM_AGENT_ENDPOINT", tt.endpoint)
			t.Setenv("BROKERIUM_DOCKER_AGENT_ENDPOINT", tt.dockerEndpoint)
			t.Setenv("BROKERIUM_AGENT_SERVER_NAMES", tt.names)
			c, err := LoadServer()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(c.AgentServerNames, ",") != strings.Join(tt.want, ",") {
				t.Errorf("AgentServerNames = %v, want %v", c.AgentServerNames, tt.want)
			}
		})
	}
}

func TestDockerAgentEndpoint(t *testing.T) {
	tests := []struct {
		name, endpoint, dockerEndpoint string
		wantAgent, wantDocker          string
		wantErrPart                    string
	}{
		{name: "defaults", wantAgent: "host.docker.internal:8443", wantDocker: "host.docker.internal:8443"},
		{name: "follows the agent endpoint", endpoint: "agents.example.com:8443",
			wantAgent: "agents.example.com:8443", wantDocker: "agents.example.com:8443"},
		{name: "Docker installation", endpoint: "docker-host.example.com:8443", dockerEndpoint: "brokerium:8443",
			wantAgent: "docker-host.example.com:8443", wantDocker: "brokerium:8443"},
		{name: "only the Docker endpoint", dockerEndpoint: "brokerium:8443",
			wantAgent: "host.docker.internal:8443", wantDocker: "brokerium:8443"},
		{name: "without a port", dockerEndpoint: "brokerium",
			wantErrPart: `BROKERIUM_DOCKER_AGENT_ENDPOINT must be host:port, not "brokerium"`},
		{name: "without a host", dockerEndpoint: ":8443", wantErrPart: "BROKERIUM_DOCKER_AGENT_ENDPOINT must be host:port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BROKERIUM_DATA_DIR", t.TempDir())
			t.Setenv("BROKERIUM_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
			t.Setenv("BROKERIUM_AGENT_ENDPOINT", tt.endpoint)
			t.Setenv("BROKERIUM_DOCKER_AGENT_ENDPOINT", tt.dockerEndpoint)
			c, err := LoadServer()
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("LoadServer error = %v, want one containing %q", err, tt.wantErrPart)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.AgentEndpoint != tt.wantAgent || c.DockerAgentEndpoint != tt.wantDocker {
				t.Errorf("AgentEndpoint = %q, DockerAgentEndpoint = %q, want %q, %q",
					c.AgentEndpoint, c.DockerAgentEndpoint, tt.wantAgent, tt.wantDocker)
			}
		})
	}
}

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
