// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHostScriptsSyntax(t *testing.T) {
	t.Parallel()
	scripts := map[string]string{"install.sh": installScript, "uninstall.sh": uninstallScript}
	for _, shell := range []string{"sh", "dash", "bash"} {
		path, err := exec.LookPath(shell)
		if err != nil {
			continue
		}
		for name, script := range scripts {
			file := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(file, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(path, "-n", file).CombinedOutput(); err != nil {
				t.Errorf("%s -n %s: %v: %s", shell, name, err, out)
			}
		}
	}
}

func TestAgentUnitStaysStoppedForDeletedNodes(t *testing.T) {
	t.Parallel()
	_, unit, _ := strings.Cut(installScript, `cat > "$AGENT_UNIT" <<EOF`)
	unit, _, _ = strings.Cut(unit, "\nEOF\n")
	if !strings.Contains(unit, "\nRestart=always\n") || !strings.Contains(unit, "\nRestartPreventExitStatus=6\n") {
		t.Errorf("the agent unit must restart the agent unless it exits with 6:%s", unit)
	}
}

// fakeTools stand in for the system tools the uninstall script runs. Units are active while
// $FAKE/active/<unit> exists and the account exists while $FAKE/account does; every call is
// appended to $FAKE/calls.
var fakeTools = map[string]string{
	"id": `if [ "$1" = -u ]; then echo 0; else [ -e "$FAKE/account" ]; fi`,
	"systemctl": `echo "systemctl $*" >> "$FAKE/calls"
case "$1" in
  is-active) [ -e "$FAKE/active/$3" ] ;;
  disable) rm -f "$FAKE/active/$3" ;;
  stop) rm -f "$FAKE/active/$2" ;;
esac`,
	"userdel":  `echo "userdel $*" >> "$FAKE/calls"; rm -f "$FAKE/account"`,
	"getent":   `[ -e "$FAKE/account" ]`,
	"groupdel": `echo "groupdel $*" >> "$FAKE/calls"`,
}

// fakeHost is a host that the install script set up below a directory.
type fakeHost struct {
	root, fake string
	layout     scriptLayout
}

func newFakeHost(t *testing.T, kafkaActive bool) *fakeHost {
	t.Helper()
	dir := t.TempDir()
	h := &fakeHost{root: filepath.Join(dir, "root"), fake: filepath.Join(dir, "fake")}
	h.layout = layoutUnder(h.root)
	bin := filepath.Join(h.fake, "bin")
	for _, d := range []string{bin, filepath.Join(h.fake, "active")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range fakeTools {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil { //nolint:gosec // test tools must be executable
			t.Fatal(err)
		}
	}
	l := h.layout
	files := map[string]string{
		l.EnvFile:       "BROKERIUM_AGENT_SERVER=brokerium:8443\nBROKERIUM_AGENT_KAFKA_DATA_DIR=" + l.DataDir + "\n",
		l.AgentUnitFile: "[Service]\n", l.KafkaUnitFile: "[Service]\n", l.SudoersFile: "rule\n",
		strings.TrimSuffix(l.SudoersLeftovers, "*") + "a1b2c3": "rule\n", l.Binary: "agent",
		filepath.Join(l.StateDir, "node.json"): "{}", filepath.Join(l.StateDir, "logs", "server.log"): "log",
		filepath.Join(l.KafkaDir, "4.3.1", "bin", "kafka-server-start.sh"): "",
		filepath.Join(l.DataDir, "meta.properties"):                        "node.id=1\n",
		filepath.Join(h.fake, "account"):                                   "",
		filepath.Join(h.fake, "active", l.AgentUnit):                       "",
	}
	if kafkaActive {
		files[filepath.Join(h.fake, "active", l.KafkaUnit)] = ""
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

// uninstall runs the uninstall script of the host with the given options and environment.
func (h *fakeHost) uninstall(t *testing.T, env []string, args ...string) (int, string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	script := filepath.Join(h.fake, "uninstall.sh")
	if err := os.WriteFile(script, []byte(hostScript(uninstallTemplate, h.layout)), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sh, append([]string{script}, args...)...)
	cmd.Env = append([]string{"PATH=" + filepath.Join(h.fake, "bin") + ":" + os.Getenv("PATH"), "FAKE=" + h.fake}, env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return exit.ExitCode(), out.String()
	case err != nil:
		t.Fatal(err)
	}
	return 0, out.String()
}

func (h *fakeHost) exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (h *fakeHost) calls(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.fake, "calls"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(raw)
}

func TestUninstallScript(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		kafkaActive bool
		args        []string
		// prepare changes the host before the script runs.
		prepare func(t *testing.T, h *fakeHost)
		env     func(h *fakeHost) []string
		exit    int
		output  string
		// removed and kept name groups of paths: agent, kafkaUnit, state, releases and data.
		removed, kept []string
	}{
		{name: "Kafka runs", kafkaActive: true, exit: 1, output: "decommission it in Brokerium first",
			kept: []string{"agent", "kafkaUnit", "state", "releases", "data"}},
		{name: "Kafka runs and may be stopped", kafkaActive: true, args: []string{"--stop-kafka"},
			output: "kept the Kafka data", removed: []string{"agent", "kafkaUnit", "state"}, kept: []string{"releases", "data"}},
		{name: "Kafka is stopped", output: "kept the Kafka releases",
			removed: []string{"agent", "kafkaUnit", "state"}, kept: []string{"releases", "data"}},
		{name: "everything", kafkaActive: true, args: []string{"--stop-kafka", "--remove-kafka", "--delete-data"},
			output: "deleted the Kafka data", removed: []string{"agent", "kafkaUnit", "state", "releases", "data"}},
		{name: "a second run", args: []string{"--remove-kafka"},
			prepare: func(t *testing.T, h *fakeHost) {
				if code, out := h.uninstall(t, nil); code != 0 {
					t.Fatalf("first run: %d %s", code, out)
				}
			},
			output: "brokerium-agent is removed from this host", removed: []string{"agent", "state", "releases"}, kept: []string{"data"}},
		{name: "the data directory after the environment file is gone", args: []string{"--delete-data"},
			prepare: func(t *testing.T, h *fakeHost) {
				if err := os.Remove(h.layout.EnvFile); err != nil {
					t.Fatal(err)
				}
			},
			exit: 1, output: "pass BROKERIUM_AGENT_KAFKA_DATA_DIR", kept: []string{"agent", "state", "data"}},
		{name: "the data directory given after the environment file is gone", args: []string{"--delete-data"},
			prepare: func(t *testing.T, h *fakeHost) {
				if err := os.Remove(h.layout.EnvFile); err != nil {
					t.Fatal(err)
				}
			},
			env:    func(h *fakeHost) []string { return []string{"BROKERIUM_AGENT_KAFKA_DATA_DIR=" + h.layout.DataDir} },
			output: "deleted the Kafka data", removed: []string{"agent", "state", "data"}},
		{name: "a data directory with a parent part", args: []string{"--delete-data"},
			prepare: func(t *testing.T, h *fakeHost) {
				env := "BROKERIUM_AGENT_KAFKA_DATA_DIR=" + h.layout.DataDir + "/../..\n"
				if err := os.WriteFile(h.layout.EnvFile, []byte(env), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			exit: 1, output: "refusing to delete", kept: []string{"agent", "state", "data"}},
		{name: "the root directory as data directory", args: []string{"--delete-data"},
			prepare: func(t *testing.T, h *fakeHost) {
				if err := os.WriteFile(h.layout.EnvFile, []byte("BROKERIUM_AGENT_KAFKA_DATA_DIR=//\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			exit: 1, output: "refusing to delete //", kept: []string{"agent", "state", "data"}},
		{name: "an unknown option", args: []string{"--purge"}, exit: 2, output: "usage:", kept: []string{"agent", "state"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newFakeHost(t, tt.kafkaActive)
			if tt.prepare != nil {
				tt.prepare(t, h)
			}
			var env []string
			if tt.env != nil {
				env = tt.env(h)
			}
			code, out := h.uninstall(t, env, tt.args...)
			if code != tt.exit || !strings.Contains(out, tt.output) {
				t.Fatalf("exit %d, want %d, with %q in the output:\n%s", code, tt.exit, tt.output, out)
			}
			l := h.layout
			paths := map[string][]string{
				"agent":     {l.AgentUnitFile, l.Binary, l.SudoersFile},
				"kafkaUnit": {l.KafkaUnitFile},
				"state":     {l.StateDir, filepath.Join(h.fake, "account")},
				"releases":  {l.KafkaDir},
				"data":      {l.DataDir},
			}
			for _, name := range tt.removed {
				for _, p := range paths[name] {
					if h.exists(p) {
						t.Errorf("%s was not removed", p)
					}
				}
			}
			for _, name := range tt.kept {
				for _, p := range paths[name] {
					if !h.exists(p) {
						t.Errorf("%s was removed", p)
					}
				}
			}
			calls := h.calls(t)
			if tt.exit != 0 && strings.Contains(calls, "disable") {
				t.Errorf("the script stopped units although it failed:\n%s", calls)
			}
			if slices.Contains(tt.removed, "agent") && !strings.Contains(calls, "systemctl disable --now brokerium-agent.service") {
				t.Errorf("the agent unit was not stopped and disabled:\n%s", calls)
			}
		})
	}
}
