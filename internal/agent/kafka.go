// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	agentv1 "github.com/bvaliev/brokerium/proto/brokerium/agent/v1"
)

var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Kafka manages the local Kafka installation and process of a node.
type Kafka struct {
	cfg Config

	mu       sync.Mutex
	cmd      *exec.Cmd
	done     chan struct{}
	lastExit string
	state    nodeState
	sizeAt   time.Time
	size     int64

	javaOnce sync.Once
	java     string
}

type nodeState struct {
	ActiveVersion string            `json:"activeVersion"`
	Installed     map[string]string `json:"installed"`
	HeapOpts      string            `json:"heapOpts"`
	Running       bool              `json:"running"`
}

func newKafka(cfg Config) *Kafka {
	k := &Kafka{cfg: cfg, state: nodeState{Installed: map[string]string{}}}
	if raw, err := os.ReadFile(k.statePath()); err == nil {
		_ = json.Unmarshal(raw, &k.state)
	}
	if k.state.Installed == nil {
		k.state.Installed = map[string]string{}
	}
	if v := detectVersion(cfg.Preinstalled); v != "" {
		k.state.Installed[v] = cfg.Preinstalled
		if k.state.ActiveVersion == "" {
			k.state.ActiveVersion = v
		}
	}
	return k
}

func (k *Kafka) statePath() string  { return filepath.Join(k.cfg.DataDir, "state.json") }
func (k *Kafka) configPath() string { return filepath.Join(k.cfg.DataDir, "server.properties") }

func (k *Kafka) saveState() {
	raw, _ := json.MarshalIndent(k.state, "", "  ")
	_ = os.WriteFile(k.statePath(), raw, 0o600)
}

// detectVersion reads the Kafka version of an installation from its kafka-clients jar.
func detectVersion(home string) string {
	if home == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(home, "libs", "kafka-clients-*.jar"))
	for _, m := range matches {
		v := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(m), "kafka-clients-"), ".jar")
		if versionRe.MatchString(v) {
			return v
		}
	}
	return ""
}

func (k *Kafka) home(version string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if version == "" {
		version = k.state.ActiveVersion
	}
	h, ok := k.state.Installed[version]
	if !ok {
		return "", fmt.Errorf("kafka %s is not installed", version)
	}
	return h, nil
}

// Install makes a Kafka release available, verifying the SHA-512 published by Apache.
func (k *Kafka) Install(ctx context.Context, version, mirror string) (string, error) {
	if !versionRe.MatchString(version) {
		return "", fmt.Errorf("invalid version %q", version)
	}
	k.mu.Lock()
	if h, ok := k.state.Installed[version]; ok {
		k.mu.Unlock()
		return "already installed at " + h, nil
	}
	k.mu.Unlock()
	file := fmt.Sprintf("kafka_2.13-%s.tgz", version)
	mirrors := []string{"https://dlcdn.apache.org/kafka", "https://downloads.apache.org/kafka", "https://archive.apache.org/dist/kafka"}
	if mirror != "" {
		mirrors = append([]string{strings.TrimRight(mirror, "/")}, mirrors...)
	}
	sumURLs := []string{
		fmt.Sprintf("https://downloads.apache.org/kafka/%s/%s.sha512", version, file),
		fmt.Sprintf("https://archive.apache.org/dist/kafka/%s/%s.sha512", version, file),
	}
	want, err := fetchSHA512(ctx, sumURLs)
	if err != nil {
		return "", err
	}
	tmp := filepath.Join(k.cfg.KafkaDir, ".download-"+version)
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, file)
	var lastErr error
	for _, m := range mirrors {
		lastErr = download(ctx, fmt.Sprintf("%s/%s/%s", m, version, file), archive)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return "", fmt.Errorf("downloading %s: %w", file, lastErr)
	}
	got, err := fileSHA512(archive)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(got, want) {
		return "", fmt.Errorf("checksum mismatch for %s", file)
	}
	dest := filepath.Join(k.cfg.KafkaDir, version)
	_ = os.RemoveAll(dest)
	if err := extractTarGz(archive, dest); err != nil {
		return "", err
	}
	if detectVersion(dest) != version {
		return "", fmt.Errorf("extracted archive does not contain Kafka %s", version)
	}
	k.mu.Lock()
	k.state.Installed[version] = dest
	k.saveState()
	k.mu.Unlock()
	return "installed " + version + " to " + dest + " (sha512 verified)", nil
}

func fetchSHA512(ctx context.Context, urls []string) (string, error) {
	var lastErr error
	for _, u := range urls {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
			continue
		}
		text := string(body)
		if i := strings.Index(text, ":"); i >= 0 {
			text = text[i+1:]
		}
		var hexOnly strings.Builder
		for _, r := range text {
			if strings.ContainsRune("0123456789abcdefABCDEF", r) {
				hexOnly.WriteRune(r)
			}
		}
		if hexOnly.Len() != 128 {
			lastErr = fmt.Errorf("%s: unexpected checksum format", u)
			continue
		}
		return strings.ToLower(hexOnly.String()), nil
	}
	return "", fmt.Errorf("fetching checksum: %w", lastErr)
}

func download(ctx context.Context, url, dest string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func fileSHA512(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractTarGz extracts an archive, stripping its top-level directory and refusing paths that
// escape the destination.
func extractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		parts := strings.SplitN(h.Name, "/", 2)
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		target := filepath.Join(dest, filepath.Clean(parts[1]))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry %q escapes the destination", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o755|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, io.LimitReader(tr, 2<<30)); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
}

// WriteConfig replaces server.properties.
func (k *Kafka) WriteConfig(props map[string]string, heap string) error {
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# Managed by brokerium-agent; changes are overwritten.\n")
	for _, key := range keys {
		fmt.Fprintf(&b, "%s=%s\n", key, props[key])
	}
	if err := os.WriteFile(k.configPath(), []byte(b.String()), 0o644); err != nil {
		return err
	}
	k.mu.Lock()
	k.state.HeapOpts = heap
	k.saveState()
	k.mu.Unlock()
	return nil
}

// Format runs kafka-storage.sh format with --ignore-formatted.
func (k *Kafka) Format(ctx context.Context, clusterID string, mode agentv1.FormatMode, initial string) (string, error) {
	home, err := k.home("")
	if err != nil {
		return "", err
	}
	args := []string{"format", "--cluster-id", clusterID, "--config", k.configPath(), "--ignore-formatted"}
	switch mode {
	case agentv1.FormatMode_FORMAT_MODE_INITIAL:
		args = append(args, "--initial-controllers", initial)
	case agentv1.FormatMode_FORMAT_MODE_STANDALONE:
		args = append(args, "--standalone")
	case agentv1.FormatMode_FORMAT_MODE_JOIN:
		args = append(args, "--no-initial-controllers")
	}
	cmd := exec.CommandContext(ctx, filepath.Join(home, "bin", "kafka-storage.sh"), args...)
	cmd.Env = append(os.Environ(), "KAFKA_HEAP_OPTS=-Xmx256m")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Start launches Kafka as a child process (or through systemd).
func (k *Kafka) Start(version string) error {
	if version != "" {
		if _, err := k.home(version); err != nil {
			return err
		}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if version != "" {
		k.state.ActiveVersion = version
	}
	home, ok := k.state.Installed[k.state.ActiveVersion]
	if !ok {
		return fmt.Errorf("kafka %s is not installed", k.state.ActiveVersion)
	}
	if k.cfg.Process == "systemd" {
		if err := k.writeUnit(home); err != nil {
			return err
		}
		k.state.Running = true
		k.saveState()
		return exec.Command("systemctl", "restart", "brokerium-kafka.service").Run()
	}
	if k.cmd != nil {
		return nil
	}
	logDir := filepath.Join(k.cfg.DataDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)
	out, err := os.Create(filepath.Join(k.cfg.DataDir, "kafka.out"))
	if err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(home, "bin", "kafka-server-start.sh"), k.configPath())
	cmd.Env = append(os.Environ(), "LOG_DIR="+logDir)
	if k.state.HeapOpts != "" {
		cmd.Env = append(cmd.Env, "KAFKA_HEAP_OPTS="+k.state.HeapOpts)
	}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		out.Close()
		return err
	}
	done := make(chan struct{})
	k.cmd, k.done = cmd, done
	k.state.Running = true
	k.saveState()
	go func() {
		err := cmd.Wait()
		out.Close()
		k.mu.Lock()
		if err != nil {
			k.lastExit = err.Error()
		} else {
			k.lastExit = "exited normally"
		}
		if k.cmd == cmd {
			k.cmd = nil
		}
		k.mu.Unlock()
		close(done)
	}()
	return nil
}

func (k *Kafka) writeUnit(home string) error {
	unit := fmt.Sprintf(`[Unit]
Description=Apache Kafka managed by brokerium-agent
After=network-online.target

[Service]
Environment=KAFKA_HEAP_OPTS=%s
Environment=LOG_DIR=%s
ExecStart=%s %s
KillSignal=SIGTERM
TimeoutStopSec=120
Restart=on-failure
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
`, k.state.HeapOpts, filepath.Join(k.cfg.DataDir, "logs"), filepath.Join(home, "bin", "kafka-server-start.sh"), k.configPath())
	if err := os.WriteFile("/etc/systemd/system/brokerium-kafka.service", []byte(unit), 0o644); err != nil {
		return err
	}
	return exec.Command("systemctl", "daemon-reload").Run()
}

// Stop shuts Kafka down gracefully (controlled shutdown), killing it after the timeout.
func (k *Kafka) Stop(timeout time.Duration) error {
	k.mu.Lock()
	if k.cfg.Process == "systemd" {
		k.state.Running = false
		k.saveState()
		k.mu.Unlock()
		return exec.Command("systemctl", "stop", "brokerium-kafka.service").Run()
	}
	cmd, done := k.cmd, k.done
	k.state.Running = false
	k.saveState()
	k.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return fmt.Errorf("kafka did not stop within %s and was killed", timeout)
	}
}

// Wipe deletes the Kafka data directory contents after stopping Kafka.
func (k *Kafka) Wipe(confirm, nodeName string) error {
	if confirm != nodeName || nodeName == "" {
		return errors.New("wipe requires confirm to equal the node name")
	}
	if err := k.Stop(2 * time.Minute); err != nil {
		return err
	}
	entries, err := os.ReadDir(k.cfg.KafkaDataDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(k.cfg.KafkaDataDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// Status reports the node state.
func (k *Kafka) Status() *agentv1.NodeStatus {
	host, _ := os.Hostname()
	k.mu.Lock()
	st := &agentv1.NodeStatus{ActiveVersion: k.state.ActiveVersion, Hostname: host, LastExit: k.lastExit}
	if k.cmd != nil && k.cmd.Process != nil {
		st.Running, st.Pid = true, int64(k.cmd.Process.Pid)
	}
	for v := range k.state.Installed {
		st.InstalledVersions = append(st.InstalledVersions, v)
	}
	systemd := k.cfg.Process == "systemd"
	k.mu.Unlock()
	if systemd {
		st.Running = exec.Command("systemctl", "is-active", "--quiet", "brokerium-kafka.service").Run() == nil
	}
	sort.Strings(st.InstalledVersions)
	st.Meta = readMeta(filepath.Join(k.cfg.KafkaDataDir, "meta.properties"))
	st.ProcessRoles = readProperty(k.configPath(), "process.roles")
	var fs unix.Statfs_t
	if unix.Statfs(k.cfg.KafkaDataDir, &fs) == nil {
		st.DiskFreeBytes = int64(uint64(fs.Bavail) * uint64(fs.Bsize)) //nolint:unconvert // field types differ between Linux and macOS
	}
	st.DataBytes = k.dataBytes()
	st.JavaVersion = k.javaVersion()
	return st
}

var javaVersionRe = regexp.MustCompile(`version "([^"]+)"`)

// javaVersion reports the runtime the Kafka scripts pick (JAVA_HOME first, then PATH).
func (k *Kafka) javaVersion() string {
	k.javaOnce.Do(func() {
		bin := "java"
		if home := os.Getenv("JAVA_HOME"); home != "" {
			bin = filepath.Join(home, "bin", "java")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, "-version").CombinedOutput()
		if err == nil {
			k.java = parseJavaVersion(string(out))
		}
	})
	return k.java
}

func parseJavaVersion(out string) string {
	if m := javaVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

func (k *Kafka) dataBytes() int64 {
	k.mu.Lock()
	if time.Since(k.sizeAt) < time.Minute {
		defer k.mu.Unlock()
		return k.size
	}
	k.mu.Unlock()
	var total int64
	_ = filepath.WalkDir(k.cfg.KafkaDataDir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	k.mu.Lock()
	k.size, k.sizeAt = total, time.Now()
	k.mu.Unlock()
	return total
}

func readMeta(path string) *agentv1.MetaProperties {
	f, err := os.Open(path)
	if err != nil {
		return &agentv1.MetaProperties{}
	}
	defer f.Close()
	m := &agentv1.MetaProperties{Formatted: true}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "cluster.id":
			m.ClusterId = strings.TrimSpace(value)
		case "node.id":
			id, _ := strconv.Atoi(strings.TrimSpace(value))
			m.NodeId = int32(id)
		case "directory.id":
			m.DirectoryId = strings.TrimSpace(value)
		}
	}
	return m
}

func readProperty(path, name string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok && strings.TrimSpace(key) == name {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// Resume restarts Kafka after an agent restart if it was running before.
func (k *Kafka) Resume() error {
	k.mu.Lock()
	running := k.state.Running && k.cfg.Process != "systemd"
	k.mu.Unlock()
	if !running {
		return nil
	}
	if _, err := os.Stat(k.configPath()); err != nil {
		return nil
	}
	return k.Start("")
}
