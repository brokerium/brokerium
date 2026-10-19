// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package config loads brokerium-server settings from the environment.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/brokerium/brokerium/internal/version"
)

// Server holds all settings of brokerium-server.
type Server struct {
	HTTPAddr         string
	GRPCAddr         string
	DatabaseURL      string
	SecretKey        []byte
	DataDir          string
	AdminUsername    string
	AdminPassword    string
	PublicURL        string
	AgentEndpoint    string
	AgentServerNames []string
	DockerHost       string
	LabNetwork       string
	AgentImage       string
	// AgentCertLifetime of agent certificates and the agent gateway certificate.
	AgentCertLifetime time.Duration
	SnapshotInterval  time.Duration
	SecureCookies     bool
	WorkerInterval    time.Duration
}

// LoadServer reads BROKERIUM_* environment variables and applies defaults suitable for the lab.
func LoadServer() (Server, error) {
	c := Server{
		HTTPAddr:          env("BROKERIUM_HTTP_ADDR", ":8080"),
		GRPCAddr:          env("BROKERIUM_GRPC_ADDR", ":8443"),
		DatabaseURL:       env("BROKERIUM_DATABASE_URL", "postgres://brokerium:brokerium@localhost:55432/brokerium?sslmode=disable"),
		DataDir:           env("BROKERIUM_DATA_DIR", ".data/server"),
		AdminUsername:     env("BROKERIUM_ADMIN_USERNAME", "admin"),
		AdminPassword:     os.Getenv("BROKERIUM_ADMIN_PASSWORD"),
		PublicURL:         env("BROKERIUM_PUBLIC_URL", "http://localhost:8080"),
		AgentEndpoint:     env("BROKERIUM_AGENT_ENDPOINT", "host.docker.internal:8443"),
		DockerHost:        env("BROKERIUM_DOCKER_HOST", "unix:///var/run/docker.sock"),
		LabNetwork:        env("BROKERIUM_LAB_NETWORK", "brokerium-lab"),
		AgentImage:        env("BROKERIUM_AGENT_IMAGE", defaultAgentImage(version.Version)),
		AgentCertLifetime: envDuration("BROKERIUM_AGENT_CERT_LIFETIME", 365*24*time.Hour),
		SecureCookies:     envBool("BROKERIUM_SECURE_COOKIES", false),
		SnapshotInterval:  envDuration("BROKERIUM_SNAPSHOT_INTERVAL", 15*time.Second),
		WorkerInterval:    envDuration("BROKERIUM_WORKER_INTERVAL", time.Second),
	}
	c.AgentServerNames = splitList(env("BROKERIUM_AGENT_SERVER_NAMES", "localhost,host.docker.internal,brokerium,brokerium-server"))
	if c.AgentCertLifetime < time.Minute {
		return c, errors.New("BROKERIUM_AGENT_CERT_LIFETIME must be at least 1m")
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
