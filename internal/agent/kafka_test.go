// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
