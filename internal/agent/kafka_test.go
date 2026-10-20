// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

func writeArchive(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kafka.tgz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []interface{ Close() error }{tw, gz, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestExtractTarGz(t *testing.T) {
	t.Parallel()
	archive := writeArchive(t, map[string]string{
		"kafka_2.13-4.4.0/bin/kafka-server-start.sh":    "#!/bin/sh\n",
		"kafka_2.13-4.4.0/libs/kafka-clients-4.4.0.jar": "jar",
	})
	dest := t.TempDir()
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatal(err)
	}
	if detectVersion(dest) != "4.4.0" {
		t.Errorf("detectVersion = %q", detectVersion(dest))
	}
	info, err := os.Stat(filepath.Join(dest, "bin", "kafka-server-start.sh"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("start script missing or not executable: %v %v", info, err)
	}
}

func TestExtractTarGzRejectsTraversal(t *testing.T) {
	t.Parallel()
	archive := writeArchive(t, map[string]string{"kafka/../../evil.sh": "boom"})
	dest := t.TempDir()
	err := extractTarGz(archive, dest)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("traversal entry accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "evil.sh")); err == nil {
		t.Error("file written outside the destination")
	}
}

func TestParseJavaVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"openjdk version \"21.0.8\" 2025-07-15 LTS\nOpenJDK Runtime Environment Temurin-21.0.8+9": "21.0.8",
		"java version \"1.8.0_402\"": "1.8.0_402",
		"command not found":          "",
	}
	for out, want := range tests {
		if got := parseJavaVersion(out); got != want {
			t.Errorf("parseJavaVersion(%q) = %q, want %q", out, got, want)
		}
	}
}

func TestWriteConfigRejectsUnsafeValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	k := newKafka(Config{DataDir: dir, KafkaDir: dir, KafkaDataDir: dir})
	tests := map[string]struct {
		props map[string]string
		heap  string
	}{
		"unit directive in the heap": {map[string]string{"node.id": "101"}, "-Xmx1g\nUser=root"},
		"JVM option in the heap":     {map[string]string{"node.id": "101"}, "-XX:OnOutOfMemoryError=id"},
		"line break in a value":      {map[string]string{"node.id": "101\nprocess.roles=controller"}, ""},
	}
	for name, tt := range tests {
		if err := k.WriteConfig(tt.props, tt.heap); err == nil {
			t.Errorf("%s: WriteConfig accepted it", name)
		}
	}
	if _, err := os.Stat(k.configPath()); !os.IsNotExist(err) {
		t.Errorf("rejected configurations must not be written: %v", err)
	}
}

func TestFormatArgs(t *testing.T) {
	t.Parallel()
	const id = "-T24WIV1qAW8jTuuHea1pw"
	common := []string{"format", "--cluster-id=" + id, "--config", "/etc/kafka/server.properties", "--ignore-formatted"}
	tests := []struct {
		name    string
		mode    agentv1.FormatMode
		initial string
		extra   []string
	}{
		{"initial", agentv1.FormatMode_FORMAT_MODE_INITIAL, "1@alpha-c1:9093:Vu6ZtOOWQmuTU2kZFGb5xw", []string{"--initial-controllers", "1@alpha-c1:9093:Vu6ZtOOWQmuTU2kZFGb5xw"}},
		{"standalone", agentv1.FormatMode_FORMAT_MODE_STANDALONE, "", []string{"--standalone"}},
		{"join", agentv1.FormatMode_FORMAT_MODE_JOIN, "", []string{"--no-initial-controllers"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatArgs(id, "/etc/kafka/server.properties", tt.mode, tt.initial)
			if want := append(slices.Clone(common), tt.extra...); !slices.Equal(got, want) {
				t.Errorf("formatArgs = %q, want %q", got, want)
			}
		})
	}
}
