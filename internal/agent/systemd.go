// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/brokerium/brokerium/internal/agentcfg"
)

// On hosts Kafka runs as kafkaUnit. An agent running as root writes the unit on every start. The
// installer writes a static unit for an agent without root, which may start, stop and restart it
// through the sudo rule in sudoersFile.
const (
	kafkaUnit   = "brokerium-kafka.service"
	sudoersFile = "/etc/sudoers.d/brokerium-agent"
)

// runFunc runs a host command with extra environment variables and returns its combined output.
type runFunc func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)

func runCommand(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // fixed programs and the installed Kafka release, typed arguments
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.CombinedOutput()
}

func lookSystemctl() string {
	if path, err := exec.LookPath("systemctl"); err == nil {
		return path
	}
	return "systemctl"
}

// unprivileged reports whether Kafka runs as the static unit of the installer.
func (k *Kafka) unprivileged() bool { return k.cfg.Process == "systemd" && !k.root }

func (k *Kafka) kafkaEnvPath() string { return filepath.Join(k.cfg.DataDir, "kafka.env") }

// currentLink is the release the static unit starts.
func (k *Kafka) currentLink() string { return filepath.Join(k.cfg.KafkaDir, "current") }

// controlUnit starts, stops or restarts the Kafka unit: directly as root, otherwise through sudo.
func (k *Kafka) controlUnit(action string) error {
	name, args := k.systemctl, []string{action, kafkaUnit}
	if !k.root {
		name, args = "sudo", append([]string{"-n", k.systemctl}, args...)
	}
	out, err := k.run(context.Background(), nil, name, args...)
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	switch {
	case !k.root && errors.Is(err, exec.ErrNotFound):
		return fmt.Errorf("%s %s: sudo is not installed; the agent runs without root and controls Kafka through sudo with the rule in %s",
			action, kafkaUnit, sudoersFile)
	case !k.root && (strings.HasPrefix(msg, "sudo:") || strings.HasPrefix(msg, "Sorry, user")):
		return fmt.Errorf("%s %s: sudo refused (%s); the agent needs the rule in %s: run the install script again",
			action, kafkaUnit, msg, sudoersFile)
	case msg != "":
		return fmt.Errorf("systemctl %s %s: %w: %s", action, kafkaUnit, err, msg)
	default:
		return fmt.Errorf("systemctl %s %s: %w", action, kafkaUnit, err)
	}
}

// writeUnit writes the Kafka unit of an agent running as root.
func (k *Kafka) writeUnit(home string) error {
	user := k.cfg.KafkaUser
	if user == "" {
		user = "kafka"
	}
	logDir := filepath.Join(k.cfg.DataDir, "logs")
	unit := fmt.Sprintf(`[Unit]
Description=Apache Kafka managed by brokerium-agent
After=network-online.target

[Service]
User=%s
Environment=KAFKA_HEAP_OPTS=%s
Environment=LOG_DIR=%s
ExecStart=%s %s
KillSignal=SIGTERM
TimeoutStopSec=120
Restart=on-failure
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
`, user, k.state.HeapOpts, logDir, filepath.Join(home, "bin", "kafka-server-start.sh"), k.configPath())
	if err := os.MkdirAll(logDir, 0o755); err != nil { //nolint:gosec // Kafka writes its logs here as the kafka account
		return err
	}
	if out, err := k.run(context.Background(), nil, "chown", "-R", user, logDir); err != nil {
		return fmt.Errorf("chown %s: %v: %s", logDir, err, out)
	}
	if err := os.WriteFile(k.unitFile, []byte(unit), 0o644); err != nil { //nolint:gosec // systemd unit files are world-readable
		return err
	}
	_, err := k.run(context.Background(), nil, k.systemctl, "daemon-reload")
	return err
}

// startUnit starts Kafka through the static unit, which runs the release behind currentLink with
// the heap options of kafka.env. The caller holds k.mu.
func (k *Kafka) startUnit(home string) error {
	if err := k.writeKafkaEnv(); err != nil {
		return err
	}
	action := "restart"
	if target, err := os.Readlink(k.currentLink()); err != nil || target != home {
		// A running JVM still opens jars through the link, so it moves only while Kafka is stopped.
		if err := k.controlUnit("stop"); err != nil {
			return err
		}
		if err := replaceSymlink(home, k.currentLink()); err != nil {
			return err
		}
		action = "start"
	}
	k.state.Running = true
	k.saveState()
	return k.controlUnit(action)
}

// writeKafkaEnv writes the environment file of the static unit. systemd reads it as root, so only
// heap options that pass the agent configuration rules go in.
func (k *Kafka) writeKafkaEnv() error {
	if err := agentcfg.ValidateHeap(k.state.HeapOpts); err != nil {
		return fmt.Errorf("writing %s: %w", k.kafkaEnvPath(), err)
	}
	env := "# Managed by brokerium-agent; changes are overwritten.\n"
	if k.state.HeapOpts != "" {
		env += "KAFKA_HEAP_OPTS=" + k.state.HeapOpts + "\n"
	}
	return writeFileAtomic(k.kafkaEnvPath(), []byte(env), 0o644)
}

// replaceSymlink points link at target with one rename, so the link is never missing.
func replaceSymlink(target, link string) error {
	tmp := filepath.Join(filepath.Dir(link), "."+filepath.Base(link)+".new")
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// prepareUnit writes kafka.env and creates a missing currentLink for the active release. A host
// whose agent ran as root before has neither, and its Kafka keeps running from the unit that agent
// wrote until it restarts.
func (k *Kafka) prepareUnit() error {
	if !k.unprivileged() {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	home, ok := k.state.Installed[k.state.ActiveVersion]
	if !ok {
		return nil
	}
	if err := k.writeKafkaEnv(); err != nil {
		return err
	}
	if _, err := os.Lstat(k.currentLink()); errors.Is(err, fs.ErrNotExist) {
		return replaceSymlink(home, k.currentLink())
	}
	return nil
}

// shareWithGroup gives the group of dir, kafka, read and write access to what the agent created
// below it, without entering directories of other owners. The umask does not cover everything:
// Kafka writes the snapshot of a new quorum through Files.createTempFile, which uses mode 0600.
// The kafka account can write to dir, so links it places there must not lead outside.
func shareWithGroup(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	uid := int64(os.Geteuid())
	return fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || int64(st.Uid) != uid {
			if d.IsDir() && path != "." {
				return fs.SkipDir
			}
			return nil
		}
		var add fs.FileMode
		switch {
		case d.IsDir():
			add = 0o070
		case d.Type().IsRegular():
			add = 0o060
		default:
			return nil
		}
		if info.Mode()&add == add {
			return nil
		}
		return root.Chmod(path, info.Mode()|add)
	})
}
