// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package config loads brokerium-server settings from the environment.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/brokerium/brokerium/internal/version"
)

// Server holds all settings of brokerium-server.
type Server struct {
	HTTPAddr      string
	GRPCAddr      string
	DatabaseURL   string
	SecretKey     []byte
	DataDir       string
	AdminUsername string
	AdminPassword string
	PublicURL     string
	AgentEndpoint string
	// AgentProxy and AgentNoProxy go into the agent install command as HTTPS_PROXY and NO_PROXY.
	AgentProxy, AgentNoProxy string
	AgentServerNames         []string
	// AgentInsecureAddr is the plaintext listener of the insecure agent transport (ADR 0017);
	// empty disables it.
	AgentInsecureAddr string
	// AgentTransport is the transport of the agent install command: mtls or insecure.
	AgentTransport string
	// AgentInsecureEndpoint is the address agents of the insecure transport dial, usually a
	// proxy that terminates TLS.
	AgentInsecureEndpoint string
	// AgentInsecureTLS is how they dial it: system, skip-verify or none.
	AgentInsecureTLS string
	// DockerProvider offers Docker clusters: auto (in the Docker installation only), on or off.
	DockerProvider string
	DockerHost     string
	LabNetwork     string
	AgentImage     string
	// DockerAgentEndpoint is the address the agents in the nodes of Docker clusters dial, by
	// default AgentEndpoint, which goes into the install commands of agents on other hosts.
	DockerAgentEndpoint string
	// AgentCertLifetime of agent certificates and the agent gateway certificate.
	AgentCertLifetime time.Duration
	SnapshotInterval  time.Duration
	SecureCookies     bool
	WorkerInterval    time.Duration
	// License is a Brokerium Enterprise license key and LicenseFile a file that holds one; both
	// take precedence over the key stored through the dashboard.
	License     string
	LicenseFile string
}

// LoadServer reads BROKERIUM_* environment variables and applies defaults suitable for the lab.
func LoadServer() (Server, error) {
	c := Server{
		HTTPAddr:              env("BROKERIUM_HTTP_ADDR", ":8080"),
		GRPCAddr:              env("BROKERIUM_GRPC_ADDR", ":8443"),
		DatabaseURL:           env("BROKERIUM_DATABASE_URL", "postgres://brokerium:brokerium@localhost:55432/brokerium?sslmode=disable"),
		DataDir:               env("BROKERIUM_DATA_DIR", ".data/server"),
		AdminUsername:         env("BROKERIUM_ADMIN_USERNAME", "admin"),
		AdminPassword:         os.Getenv("BROKERIUM_ADMIN_PASSWORD"),
		PublicURL:             env("BROKERIUM_PUBLIC_URL", "http://localhost:8080"),
		AgentEndpoint:         env("BROKERIUM_AGENT_ENDPOINT", "host.docker.internal:8443"),
		AgentProxy:            os.Getenv("BROKERIUM_AGENT_PROXY"),
		AgentNoProxy:          os.Getenv("BROKERIUM_AGENT_NO_PROXY"),
		AgentInsecureAddr:     os.Getenv("BROKERIUM_AGENT_INSECURE_ADDR"),
		AgentTransport:        env("BROKERIUM_AGENT_TRANSPORT", "mtls"),
		AgentInsecureEndpoint: os.Getenv("BROKERIUM_AGENT_INSECURE_ENDPOINT"),
		AgentInsecureTLS:      env("BROKERIUM_AGENT_INSECURE_TLS", "system"),
		DockerProvider:        env("BROKERIUM_DOCKER_PROVIDER", "auto"),
		DockerHost:            env("BROKERIUM_DOCKER_HOST", "unix:///var/run/docker.sock"),
		LabNetwork:            env("BROKERIUM_LAB_NETWORK", "brokerium-lab"),
		AgentImage:            env("BROKERIUM_AGENT_IMAGE", defaultAgentImage(version.Version)),
		DockerAgentEndpoint:   os.Getenv("BROKERIUM_DOCKER_AGENT_ENDPOINT"),
		AgentCertLifetime:     envDuration("BROKERIUM_AGENT_CERT_LIFETIME", 365*24*time.Hour),
		SecureCookies:         envBool("BROKERIUM_SECURE_COOKIES", false),
		SnapshotInterval:      envDuration("BROKERIUM_SNAPSHOT_INTERVAL", 15*time.Second),
		WorkerInterval:        envDuration("BROKERIUM_WORKER_INTERVAL", time.Second),
		License:               strings.TrimSpace(os.Getenv("BROKERIUM_LICENSE")),
		LicenseFile:           os.Getenv("BROKERIUM_LICENSE_FILE"),
	}
	if c.AgentCertLifetime < time.Minute {
		return c, errors.New("BROKERIUM_AGENT_CERT_LIFETIME must be at least 1m")
	}
	if err := c.checkAgentTransport(); err != nil {
		return c, err
	}
	if err := c.checkDocker(); err != nil {
		return c, err
	}
	if c.DockerAgentEndpoint == "" {
		c.DockerAgentEndpoint = c.AgentEndpoint
	}
	c.AgentServerNames = splitList(env("BROKERIUM_AGENT_SERVER_NAMES", "localhost,host.docker.internal,brokerium,brokerium-server"))
	// Agents check the gateway certificate against the host they dial.
	for _, endpoint := range []string{c.AgentEndpoint, c.DockerAgentEndpoint} {
		if host, _, err := net.SplitHostPort(endpoint); err == nil && host != "" && !slices.Contains(c.AgentServerNames, host) {
			c.AgentServerNames = append(c.AgentServerNames, host)
		}
	}

	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return c, fmt.Errorf("creating data dir: %w", err)
	}
	key, err := loadSecretKey(os.Getenv("BROKERIUM_SECRET_KEY"), filepath.Join(c.DataDir, "secret.key"))
	if err != nil {
		return c, err
	}
	c.SecretKey = key
	return c, nil
}

// checkAgentTransport checks the settings of the insecure agent transport and of the install
// command.
func (c Server) checkAgentTransport() error {
	switch {
	case c.AgentTransport != "mtls" && c.AgentTransport != "insecure":
		return fmt.Errorf("BROKERIUM_AGENT_TRANSPORT must be mtls or insecure, not %q", c.AgentTransport)
	case c.AgentInsecureTLS != "system" && c.AgentInsecureTLS != "skip-verify" && c.AgentInsecureTLS != "none":
		return fmt.Errorf("BROKERIUM_AGENT_INSECURE_TLS must be system, skip-verify or none, not %q", c.AgentInsecureTLS)
	case c.AgentInsecureEndpoint != "" && !hostPort(c.AgentInsecureEndpoint):
		return fmt.Errorf("BROKERIUM_AGENT_INSECURE_ENDPOINT must be host:port, not %q", c.AgentInsecureEndpoint)
	case c.AgentTransport == "insecure" && c.AgentInsecureAddr == "":
		return errors.New("BROKERIUM_AGENT_TRANSPORT=insecure needs the insecure listener, BROKERIUM_AGENT_INSECURE_ADDR")
	case c.AgentTransport == "insecure" && c.AgentInsecureEndpoint == "":
		return errors.New("BROKERIUM_AGENT_TRANSPORT=insecure needs BROKERIUM_AGENT_INSECURE_ENDPOINT, the address agents dial")
	}
	return nil
}

// checkDocker checks the settings of Docker clusters.
func (c Server) checkDocker() error {
	switch {
	case c.DockerProvider != "auto" && c.DockerProvider != "on" && c.DockerProvider != "off":
		return fmt.Errorf("BROKERIUM_DOCKER_PROVIDER must be auto, on or off, not %q", c.DockerProvider)
	case c.DockerAgentEndpoint != "" && !hostPort(c.DockerAgentEndpoint):
		return fmt.Errorf("BROKERIUM_DOCKER_AGENT_ENDPOINT must be host:port, not %q", c.DockerAgentEndpoint)
	}
	return nil
}

func hostPort(s string) bool {
	host, port, err := net.SplitHostPort(s)
	return err == nil && host != "" && port != ""
}

// loadSecretKey decodes an explicit base64 key or creates a random one in the data dir.
func loadSecretKey(explicit, path string) ([]byte, error) {
	if explicit != "" {
		key, err := base64.StdEncoding.DecodeString(explicit)
		if err != nil || len(key) != 32 {
			return nil, errors.New("BROKERIUM_SECRET_KEY must be 32 bytes encoded as base64")
		}
		return key, nil
	}
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // path comes from BROKERIUM_DATA_DIR
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("invalid secret key in %s", path)
		}
		return key, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("writing secret key: %w", err)
	}
	return key, nil
}

func env(name, def string) string {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v
	}
	return def
}

func envBool(name string, def bool) bool {
	v, err := strconv.ParseBool(os.Getenv(name))
	if err != nil {
		return def
	}
	return v
}

func envDuration(name string, def time.Duration) time.Duration {
	v, err := time.ParseDuration(os.Getenv(name))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

var releaseRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// defaultAgentImage is the lab node image published with this server version: <version>-lab for
// releases and lab, built from main, for everything else.
func defaultAgentImage(serverVersion string) string {
	if releaseRe.MatchString(serverVersion) {
		return "bvaliev/brokerium-agent:" + strings.TrimPrefix(serverVersion, "v") + "-lab"
	}
	return "bvaliev/brokerium-agent:lab"
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
