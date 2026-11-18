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
	"io/fs"
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

	"github.com/brokerium/brokerium/internal/agentcfg"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Kafka manages the local Kafka installation and process of a node.
type Kafka struct {
	cfg Config
	// root agents write the Kafka unit themselves. Agents without root control the unit of the
	// installer through sudo and reach the Kafka directories through the kafka group.
	root      bool
	run       runFunc
	systemctl string
	unitFile  string

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
	k := &Kafka{cfg: cfg, state: nodeState{Installed: map[string]string{}}, root: os.Geteuid() == 0,
		run: runCommand, systemctl: lookSystemctl(), unitFile: "/etc/systemd/system/" + kafkaUnit}
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

func (k *Kafka) statePath() string { return filepath.Join(k.cfg.DataDir, "state.json") }

func (k *Kafka) configPath() string {
	if k.cfg.KafkaConfig != "" {
		return k.cfg.KafkaConfig
	}
	return filepath.Join(k.cfg.DataDir, "server.properties")
}

// errAdopted refuses changes that belong to the operator of an adopted node.
var errAdopted = errors.New("this node was adopted with --kafka-config; its configuration and storage are managed outside Brokerium")

// Adopted reports whether the node runs with an operator-owned configuration.
func (k *Kafka) Adopted() bool { return k.cfg.KafkaConfig != "" }

// dataDir is the first log directory: from the adopted configuration, else the agent setting.
func (k *Kafka) dataDir() string {
	if k.Adopted() {
		if dirs := readProperty(k.cfg.KafkaConfig, "log.dirs"); dirs != "" {
			return strings.TrimSpace(strings.Split(dirs, ",")[0])
		}
		if dir := readProperty(k.cfg.KafkaConfig, "log.dir"); dir != "" {
			return dir
		}
	}
	return k.cfg.KafkaDataDir
}

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
	file, archives, sumURLs := release(version, mirror)
	want, err := fetchSHA512(ctx, sumURLs, stallTimeout)
	if err != nil {
		return "", err
	}
	tmp := filepath.Join(k.cfg.KafkaDir, ".download-"+version)
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil { //nolint:gosec // releases must be readable by the kafka account
		return "", err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, file)
	var lastErr error
	for _, u := range archives {
		lastErr = download(ctx, u, archive, stallTimeout)
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
	if k.state.ActiveVersion == "" {
		// The first release on a fresh host is what formatting and the first start use.
		k.state.ActiveVersion = version
	}
	k.saveState()
	k.mu.Unlock()
	return "installed " + version + " to " + dest + " (sha512 verified)", nil
}

// release names the archive of a Kafka release and lists the URLs of the archive, the mirror
// first, and of its SHA-512 checksum, which only Apache publishes. A mirror has the layout of the
// Apache mirrors: <mirror>/<version>/kafka_2.13-<version>.tgz.
func release(version, mirror string) (file string, archives, checksums []string) {
	file = fmt.Sprintf("kafka_2.13-%s.tgz", version)
	bases := []string{"https://dlcdn.apache.org/kafka", "https://downloads.apache.org/kafka", "https://archive.apache.org/dist/kafka"}
	if mirror != "" {
		bases = append([]string{strings.TrimRight(mirror, "/")}, bases...)
	}
	for _, b := range bases {
		archives = append(archives, fmt.Sprintf("%s/%s/%s", b, version, file))
	}
	checksums = []string{
		fmt.Sprintf("https://downloads.apache.org/kafka/%s/%s.sha512", version, file),
		fmt.Sprintf("https://archive.apache.org/dist/kafka/%s/%s.sha512", version, file),
	}
	return file, archives, checksums
}

// stallTimeout is how long a download may receive no data before the agent gives up on the
// mirror and tries the next one.
const stallTimeout = time.Minute

var errStalled = errors.New("no data received")

// fetch starts a GET request that fails when no data arrives for the stall duration, also
// while it waits for the response. The caller closes the body.
func fetch(ctx context.Context, url string, stall time.Duration) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	timer := time.AfterFunc(stall, func() { cancel(fmt.Errorf("%s: %w for %s", url, errStalled, stall)) })
	fail := func(err error) (io.ReadCloser, error) {
		timer.Stop()
		cancel(nil)
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fail(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fail(stallCause(ctx, err))
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return fail(fmt.Errorf("%s: HTTP %d", url, resp.StatusCode))
	}
	return &watchedBody{body: resp.Body, ctx: ctx, cancel: cancel, timer: timer, stall: stall}, nil
}

// stallCause reports a stall instead of the bare cancellation it caused.
func stallCause(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); errors.Is(cause, errStalled) {
		return cause
	}
	return err
}

// watchedBody restarts the stall timer whenever data arrives.
type watchedBody struct {
	body   io.ReadCloser
	ctx    context.Context
	cancel context.CancelCauseFunc
	timer  *time.Timer
	stall  time.Duration
}

func (w *watchedBody) Read(p []byte) (int, error) {
	n, err := w.body.Read(p)
	if n > 0 {
		w.timer.Reset(w.stall)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		err = stallCause(w.ctx, err)
	}
	return n, err
}

func (w *watchedBody) Close() error {
	w.timer.Stop()
	w.cancel(nil)
	return w.body.Close()
}

func fetchSHA512(ctx context.Context, urls []string, stall time.Duration) (string, error) {
	var lastErr error
	for _, u := range urls {
		body, err := fetch(ctx, u, stall)
		if err != nil {
			lastErr = err
			continue
		}
		data, err := io.ReadAll(io.LimitReader(body, 4096))
		_ = body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		text := string(data)
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

func download(ctx context.Context, url, dest string, stall time.Duration) error {
	body, err := fetch(ctx, url, stall)
	if err != nil {
		return err
	}
	defer body.Close()
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
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
			perm := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				perm = 0o755
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, io.LimitReader(tr, 2<<30)); err != nil {
				_ = out.Close()
				return err
			}
			_ = out.Close()
		}
	}
}

// WriteConfig replaces server.properties.
func (k *Kafka) WriteConfig(props map[string]string, heap string) error {
	if k.Adopted() {
		return errAdopted
	}
	if err := agentcfg.Validate(props, heap); err != nil {
		return err
	}
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
	if err := os.WriteFile(k.configPath(), []byte(b.String()), 0o644); err != nil { //nolint:gosec // Kafka runs as the kafka account and reads it
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
	if k.Adopted() {
		return "", errAdopted
	}
	home, err := k.home("")
	if err != nil {
		return "", err
	}
	name, args := filepath.Join(home, "bin", "kafka-storage.sh"), formatArgs(clusterID, k.configPath(), mode, initial)
	if k.unprivileged() {
		name, args = "/bin/sh", append([]string{"-c", `umask 0007 && exec "$0" "$@"`, name}, args...)
	}
	out, err := k.run(ctx, []string{"KAFKA_HEAP_OPTS=-Xmx256m"}, name, args...)
	if err != nil {
		return strings.TrimSpace(string(out)), err
	}
	switch {
	case k.unprivileged():
		if err := shareWithGroup(k.dataDir()); err != nil {
			return strings.TrimSpace(string(out)), fmt.Errorf("opening %s to the kafka group: %w", k.dataDir(), err)
		}
	case k.cfg.Process == "systemd" && k.root:
		// The agent formats as root; the Kafka unit runs as the kafka account.
		user := k.cfg.KafkaUser
		if user == "" {
			user = "kafka"
		}
		if chown, cerr := k.run(ctx, nil, "chown", "-R", user, k.dataDir()); cerr != nil {
			return strings.TrimSpace(string(chown)), fmt.Errorf("chown %s: %w", k.dataDir(), cerr)
		}
	}
	return strings.TrimSpace(string(out)), nil
}

// formatArgs builds the kafka-storage.sh format arguments. The cluster id is joined with "=" so
// that an id starting with "-" is not read as an option.
func formatArgs(clusterID, config string, mode agentv1.FormatMode, initial string) []string {
	args := []string{"format", "--cluster-id=" + clusterID, "--config", config, "--ignore-formatted"}
	switch mode {
	case agentv1.FormatMode_FORMAT_MODE_INITIAL:
		args = append(args, "--initial-controllers", initial)
	case agentv1.FormatMode_FORMAT_MODE_STANDALONE:
		args = append(args, "--standalone")
	case agentv1.FormatMode_FORMAT_MODE_JOIN:
		args = append(args, "--no-initial-controllers")
	}
	return args
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
		if !k.root {
			return k.startUnit(home)
		}
		if err := k.writeUnit(home); err != nil {
			return err
		}
		k.state.Running = true
		k.saveState()
		return k.controlUnit("restart")
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
	cmd := exec.Command(filepath.Join(home, "bin", "kafka-server-start.sh"), k.configPath()) //nolint:gosec // the installed Kafka release
	cmd.Env = append(os.Environ(), "LOG_DIR="+logDir)
	if k.state.HeapOpts != "" {
		cmd.Env = append(cmd.Env, "KAFKA_HEAP_OPTS="+k.state.HeapOpts)
	}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = out.Close()
		return err
	}
	done := make(chan struct{})
	k.cmd, k.done = cmd, done
	k.state.Running = true
	k.saveState()
	go func() {
		err := cmd.Wait()
		_ = out.Close()
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

// Stop shuts Kafka down gracefully (controlled shutdown), killing it after the timeout, and
// records that it should stay stopped.
func (k *Kafka) Stop(timeout time.Duration) error { return k.stop(timeout, false) }

// Shutdown stops a child-process Kafka because the agent itself exits; it starts again with
// the agent. Under systemd Kafka does not depend on the agent and keeps running.
func (k *Kafka) Shutdown(timeout time.Duration) error {
	if k.cfg.Process == "systemd" {
		return nil
	}
	return k.stop(timeout, true)
}

func (k *Kafka) stop(timeout time.Duration, keepDesired bool) error {
	k.mu.Lock()
	if k.cfg.Process == "systemd" {
		k.state.Running = false
		k.saveState()
		k.mu.Unlock()
		return k.controlUnit("stop")
	}
	cmd, done := k.cmd, k.done
	if !keepDesired {
		k.state.Running = false
		k.saveState()
	}
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
	if k.Adopted() {
		return errAdopted
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
			if k.unprivileged() && errors.Is(err, fs.ErrPermission) {
				return fmt.Errorf("%w: the agent deletes Kafka data through the kafka group; run the install script again to give the group access to %s",
					err, k.cfg.KafkaDataDir)
			}
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
		_, err := k.run(context.Background(), nil, k.systemctl, "is-active", "--quiet", kafkaUnit)
		st.Running = err == nil
	}
	sort.Strings(st.InstalledVersions)
	st.Meta = readMeta(filepath.Join(k.dataDir(), "meta.properties"))
	st.ProcessRoles = readProperty(k.configPath(), "process.roles")
	var fs unix.Statfs_t
	if unix.Statfs(k.dataDir(), &fs) == nil {
		st.DiskFreeBytes = int64(uint64(fs.Bavail) * uint64(fs.Bsize)) //nolint:unconvert,gosec // field types differ between Linux and macOS; disks are far below 2^63 bytes
	}
	st.DataBytes = k.dataBytes()
	st.JavaVersion = k.javaVersion()
	st.Adopted = k.Adopted()
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
		out, err := exec.CommandContext(ctx, bin, "-version").CombinedOutput() //nolint:gosec // the Java runtime the Kafka scripts use
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
	_ = filepath.WalkDir(k.dataDir(), func(_ string, d os.DirEntry, err error) error {
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
			id, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
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

// Resume starts Kafka after an agent or host restart if it was running before. Kafka under
// systemd outlives agent restarts, so its unit starts only when it is not active, as after a
// host reboot: the unit is not enabled, because a node the server stopped must stay stopped.
func (k *Kafka) Resume() error {
	k.mu.Lock()
	running, systemd := k.state.Running, k.cfg.Process == "systemd"
	k.mu.Unlock()
	if !running {
		return nil
	}
	if _, err := os.Stat(k.configPath()); err != nil {
		return nil
	}
	if systemd {
		if _, err := k.run(context.Background(), nil, k.systemctl, "is-active", "--quiet", kafkaUnit); err == nil {
			return nil
		}
	}
	return k.Start("")
}
