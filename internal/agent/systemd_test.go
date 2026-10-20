// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// commands records the host commands a Kafka runs and answers them with reply.
type commands struct {
	mu    sync.Mutex
	calls []string
	reply func(name string, args []string) ([]byte, error)
}

func (c *commands) run(_ context.Context, _ []string, name string, args ...string) ([]byte, error) {
	c.mu.Lock()
	c.calls = append(c.calls, strings.Join(append([]string{name}, args...), " "))
	reply := c.reply
	c.mu.Unlock()
	if reply == nil {
		return nil, nil
	}
	return reply(name, args)
}

// take returns the commands run since the last call.
func (c *commands) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.calls
	c.calls = nil
	return calls
}

// newUnitKafka returns a Kafka under systemd with releases 4.3.1 (active) and 4.4.0.
func newUnitKafka(t *testing.T, root bool) (*Kafka, *commands) {
	t.Helper()
	cfg := Config{DataDir: t.TempDir(), KafkaDir: t.TempDir(), KafkaDataDir: t.TempDir(), Process: "systemd"}
	k := newKafka(cfg)
	cmds := &commands{}
	k.root, k.run, k.systemctl = root, cmds.run, "/usr/bin/systemctl"
	k.unitFile = filepath.Join(t.TempDir(), kafkaUnit)
	k.javaOnce.Do(func() {})
	for _, v := range []string{"4.3.1", "4.4.0"} {
		k.state.Installed[v] = filepath.Join(cfg.KafkaDir, v)
	}
	k.state.ActiveVersion, k.state.HeapOpts = "4.3.1", "-Xmx512m -Xms512m"
	return k, cmds
}

func sudo(action string) string {
	return "sudo -n /usr/bin/systemctl " + action + " " + kafkaUnit
}

func TestStartWithoutRoot(t *testing.T) {
	t.Parallel()
	k, cmds := newUnitKafka(t, false)
	steps := []struct {
		name, version, release string
		calls                  []string
	}{
		{"first start links the release", "", "4.3.1", []string{sudo("stop"), sudo("start")}},
		{"same release restarts", "", "4.3.1", []string{sudo("restart")}},
		{"upgrade moves the link while Kafka is stopped", "4.4.0", "4.4.0", []string{sudo("stop"), sudo("start")}},
		{"same release by name restarts", "4.4.0", "4.4.0", []string{sudo("restart")}},
	}
	for _, s := range steps {
		if err := k.Start(s.version); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if got := cmds.take(); !slices.Equal(got, s.calls) {
			t.Errorf("%s: commands %q, want %q", s.name, got, s.calls)
		}
		if target, err := os.Readlink(k.currentLink()); err != nil || target != k.state.Installed[s.release] {
			t.Errorf("%s: current points at %q (%v), want release %s", s.name, target, err, s.release)
		}
	}
	env, err := os.ReadFile(k.kafkaEnvPath())
	if err != nil || !strings.HasSuffix(string(env), "\nKAFKA_HEAP_OPTS=-Xmx512m -Xms512m\n") {
		t.Errorf("kafka.env = %q (%v)", env, err)
	}
	if _, err := os.Stat(k.unitFile); !os.IsNotExist(err) {
		t.Errorf("an agent without root must not write the unit: %v", err)
	}
	entries, _ := os.ReadDir(k.cfg.KafkaDir)
	if len(entries) != 1 || entries[0].Name() != "current" {
		t.Errorf("kafka dir holds %v, want only the current link", entries)
	}
}

func TestStopStatusAndWipeWithoutRoot(t *testing.T) {
	t.Parallel()
	k, cmds := newUnitKafka(t, false)
	if err := k.Stop(time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, want := cmds.take(), []string{sudo("stop")}; !slices.Equal(got, want) {
		t.Errorf("Stop ran %q, want %q", got, want)
	}
	if st := k.Status(); !st.GetRunning() {
		t.Error("an active unit must report Kafka as running")
	}
	if got, want := cmds.take(), []string{"/usr/bin/systemctl is-active --quiet " + kafkaUnit}; !slices.Equal(got, want) {
		t.Errorf("Status ran %q, want %q", got, want)
	}
	for _, name := range []string{"meta.properties", "topic-0/00000000000000000000.log"} {
		path := filepath.Join(k.cfg.KafkaDataDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o770); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o660); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.Wipe("vm-b101", "vm-b101"); err != nil {
		t.Fatal(err)
	}
	if got, want := cmds.take(), []string{sudo("stop")}; !slices.Equal(got, want) {
		t.Errorf("Wipe ran %q, want %q", got, want)
	}
	if entries, _ := os.ReadDir(k.cfg.KafkaDataDir); len(entries) != 0 {
		t.Errorf("data directory still holds %v", entries)
	}
}

func TestFormatWithoutRoot(t *testing.T) {
	t.Parallel()
	k, cmds := newUnitKafka(t, false)
	snapshot := filepath.Join(k.cfg.KafkaDataDir, "__cluster_metadata-0", "00000000000000000000-0000000000.checkpoint")
	cmds.reply = func(string, []string) ([]byte, error) {
		// Like kafka-storage.sh: meta.properties under the umask, the snapshot with mode 0600.
		if err := os.WriteFile(filepath.Join(k.cfg.KafkaDataDir, "meta.properties"), []byte("node.id=1\n"), 0o660); err != nil {
			return nil, err
		}
		if err := os.Mkdir(filepath.Dir(snapshot), 0o700); err != nil {
			return nil, err
		}
		return []byte("Formatting metadata directory"), os.WriteFile(snapshot, nil, 0o600)
	}
	out, err := k.Format(context.Background(), "-T24WIV1qAW8jTuuHea1pw", agentv1.FormatMode_FORMAT_MODE_STANDALONE, "")
	if err != nil || out != "Formatting metadata directory" {
		t.Fatalf("Format = %q, %v", out, err)
	}
	want := []string{`/bin/sh -c umask 0007 && exec "$0" "$@" ` + filepath.Join(k.state.Installed["4.3.1"], "bin", "kafka-storage.sh") +
		" format --cluster-id=-T24WIV1qAW8jTuuHea1pw --config " + k.configPath() + " --ignore-formatted --standalone"}
	if got := cmds.take(); !slices.Equal(got, want) {
		t.Errorf("Format ran %q, want %q", got, want)
	}
	for path, perm := range map[string]os.FileMode{
		filepath.Join(k.cfg.KafkaDataDir, "meta.properties"): 0o060, filepath.Dir(snapshot): 0o070, snapshot: 0o060,
	} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm()&perm != perm {
			t.Errorf("%s: mode %v (%v), want the group bits %v", path, info.Mode().Perm(), err, perm)
		}
	}
}

func TestShareWithGroupStaysInside(t *testing.T) {
	t.Parallel()
	dir, outside := t.TempDir(), filepath.Join(t.TempDir(), "agent.pem")
	if err := os.WriteFile(outside, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"key": outside, "dir": filepath.Dir(outside)} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := shareWithGroup(dir); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(outside); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("a file outside the data directory changed to %v (%v)", info.Mode().Perm(), err)
	}
}

func TestSudoErrors(t *testing.T) {
	t.Parallel()
	exit := errors.New("exit status 1")
	tests := []struct {
		name     string
		out      string
		err      error
		want     string
		sudoRule bool
	}{
		{"no rule", "sudo: a password is required\n", exit, "sudo refused (sudo: a password is required)", true},
		{"no new privileges", `sudo: The "no new privileges" flag is set, which prevents sudo from running as root.`, exit, "no new privileges", true},
		{"sudo missing", "", &exec.Error{Name: "sudo", Err: exec.ErrNotFound}, "sudo is not installed", true},
		{"unit failed", "Job for brokerium-kafka.service failed because the control process exited with error code.", exit,
			"systemctl start brokerium-kafka.service: exit status 1: Job for brokerium-kafka.service failed", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			k, cmds := newUnitKafka(t, false)
			cmds.reply = func(string, []string) ([]byte, error) { return []byte(tt.out), tt.err }
			err := k.controlUnit("start")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("controlUnit = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), sudoersFile) != tt.sudoRule {
				t.Errorf("mentions %s: %v, want %v", sudoersFile, err, tt.sudoRule)
			}
		})
	}
}

func TestStartAndFormatAsRoot(t *testing.T) {
	t.Parallel()
	k, cmds := newUnitKafka(t, true)
	if err := k.Start(""); err != nil {
		t.Fatal(err)
	}
	home := k.state.Installed["4.3.1"]
	logDir := filepath.Join(k.cfg.DataDir, "logs")
	want := []string{"chown -R kafka " + logDir, "/usr/bin/systemctl daemon-reload", "/usr/bin/systemctl restart " + kafkaUnit}
	if got := cmds.take(); !slices.Equal(got, want) {
		t.Errorf("Start ran %q, want %q", got, want)
	}
	unit, err := os.ReadFile(k.unitFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"User=kafka", "Environment=KAFKA_HEAP_OPTS=-Xmx512m -Xms512m", "Environment=LOG_DIR=" + logDir,
		"ExecStart=" + filepath.Join(home, "bin", "kafka-server-start.sh") + " " + k.configPath()} {
		if !strings.Contains(string(unit), "\n"+line+"\n") {
			t.Errorf("unit lacks %q:\n%s", line, unit)
		}
	}
	for _, path := range []string{k.currentLink(), k.kafkaEnvPath()} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("an agent running as root must not create %s: %v", path, err)
		}
	}
	if _, err := k.Format(context.Background(), "id", agentv1.FormatMode_FORMAT_MODE_JOIN, ""); err != nil {
		t.Fatal(err)
	}
	want = []string{filepath.Join(home, "bin", "kafka-storage.sh") + " format --cluster-id=id --config " + k.configPath() +
		" --ignore-formatted --no-initial-controllers", "chown -R kafka " + k.cfg.KafkaDataDir}
	if got := cmds.take(); !slices.Equal(got, want) {
		t.Errorf("Format ran %q, want %q", got, want)
	}
}

func TestPrepareUnit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		root   bool
		linked string
		want   string
	}{
		{"converted host", false, "", "4.3.1"},
		{"link already set", false, "4.4.0", "4.4.0"},
		{"agent running as root", true, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			k, cmds := newUnitKafka(t, tt.root)
			if tt.linked != "" {
				if err := os.Symlink(k.state.Installed[tt.linked], k.currentLink()); err != nil {
					t.Fatal(err)
				}
			}
			if err := k.prepareUnit(); err != nil {
				t.Fatal(err)
			}
			if got := cmds.take(); len(got) != 0 {
				t.Errorf("prepareUnit ran %q", got)
			}
			target, _ := os.Readlink(k.currentLink())
			_, envErr := os.Stat(k.kafkaEnvPath())
			if tt.want == "" {
				if target != "" || envErr == nil {
					t.Errorf("link %q, kafka.env %v: an agent running as root keeps both to its own unit", target, envErr)
				}
				return
			}
			if target != k.state.Installed[tt.want] || envErr != nil {
				t.Errorf("link %q, kafka.env %v, want release %s and kafka.env", target, envErr, tt.want)
			}
		})
	}
}

func TestWriteKafkaEnvRejectsUnsafeHeap(t *testing.T) {
	t.Parallel()
	k, _ := newUnitKafka(t, false)
	k.state.HeapOpts = "-Xmx1g\nLD_PRELOAD=/tmp/x.so"
	if err := k.writeKafkaEnv(); err == nil {
		t.Fatal("unsafe heap options were accepted")
	}
	if _, err := os.Stat(k.kafkaEnvPath()); !os.IsNotExist(err) {
		t.Errorf("rejected heap options must not be written: %v", err)
	}
}
