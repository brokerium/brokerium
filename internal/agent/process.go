// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"io/fs"
	"os/user"
	"path"
	"regexp"
	"strconv"
	"strings"

	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// kafkaMainClass is the main class of Kafka brokers and controllers.
const kafkaMainClass = "kafka.Kafka"

// userHZ is the unit of the start times in /proc/<pid>/stat on Linux for amd64 and arm64.
const userHZ = 100

// javaValueOptions are the java launcher options whose value is the next argument.
var javaValueOptions = map[string]bool{
	"-cp": true, "-classpath": true, "--class-path": true, "-p": true, "--module-path": true,
	"--upgrade-module-path": true, "--add-modules": true, "--limit-modules": true, "--add-reads": true,
	"--add-exports": true, "--add-opens": true, "--patch-module": true, "--enable-native-access": true,
}

var (
	clientsJarRe = regexp.MustCompile(`^kafka-clients-(\d+\.\d+\.\d+)\.jar$`)
	coreJarRe    = regexp.MustCompile(`^kafka_2\.1[23]-(\d+\.\d+\.\d+)\.jar$`)
)

// splitCmdline splits /proc/<pid>/cmdline into the arguments of the process.
func splitCmdline(raw []byte) []string {
	s := strings.TrimRight(string(raw), "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}

// mainClass is the main class of a java command line: its first argument after the launcher
// that is neither an option nor the value of one. It is empty for -jar.
func mainClass(args []string) string {
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-jar":
			return ""
		case javaValueOptions[a]:
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a
		}
	}
	return ""
}

// classpath returns the -cp, -classpath or --class-path value of a java command line.
func classpath(args []string) string {
	for i := 1; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-cp" || a == "-classpath" || a == "--class-path":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--class-path="):
			return strings.TrimPrefix(a, "--class-path=")
		}
	}
	return ""
}

// kafkaVersion reads the Kafka version from the classpath of a Kafka command line: the version
// of the first kafka-clients jar, else of the first Kafka core jar. mixed reports jars of
// several Kafka versions.
func kafkaVersion(args []string) (version string, mixed bool) {
	var core string
	seen := map[string]bool{}
	for _, entry := range strings.Split(classpath(args), ":") {
		name := path.Base(entry)
		if m := clientsJarRe.FindStringSubmatch(name); m != nil {
			seen[m[1]] = true
			if version == "" {
				version = m[1]
			}
		} else if m := coreJarRe.FindStringSubmatch(name); m != nil {
			seen[m[1]] = true
			if core == "" {
				core = m[1]
			}
		}
	}
	if version == "" {
		version = core
	}
	return version, len(seen) > 1
}

// procStat holds the fields of /proc/<pid>/stat the agent uses.
type procStat struct {
	ppid int
	// start is the start time in clock ticks after boot.
	start int64
}

// parseStat parses /proc/<pid>/stat. The command name in parentheses may contain spaces and
// parentheses, so the fields are counted after its last closing parenthesis.
func parseStat(raw string) (procStat, bool) {
	i := strings.LastIndexByte(raw, ')')
	if i < 0 {
		return procStat{}, false
	}
	// fields[0] is field 3 of proc(5), the state; ppid is field 4 and starttime field 22.
	fields := strings.Fields(raw[i+1:])
	if len(fields) < 20 {
		return procStat{}, false
	}
	ppid, err1 := strconv.Atoi(fields[1])
	start, err2 := strconv.ParseInt(fields[19], 10, 64)
	if err1 != nil || err2 != nil {
		return procStat{}, false
	}
	return procStat{ppid: ppid, start: start}, true
}

// parseBootTime returns the btime of /proc/stat, the boot time in Unix seconds.
func parseBootTime(raw string) int64 {
	for line := range strings.Lines(raw) {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			t, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return t
		}
	}
	return 0
}

// procFS reads processes from a proc file system. Entries of processes that end or that the
// agent may not read are skipped.
type procFS struct{ fsys fs.FS }

func (p procFS) read(pid int, name string) (string, error) {
	raw, err := fs.ReadFile(p.fsys, strconv.Itoa(pid)+"/"+name)
	return string(raw), err
}

func (p procFS) pids() []int {
	entries, err := fs.ReadDir(p.fsys, ".")
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && pid > 0 {
			out = append(out, pid)
		}
	}
	return out
}

func (p procFS) args(pid int) []string {
	raw, err := p.read(pid, "cmdline")
	if err != nil {
		return nil
	}
	return splitCmdline([]byte(raw))
}

func (p procFS) stat(pid int) (procStat, bool) {
	raw, err := p.read(pid, "stat")
	if err != nil {
		return procStat{}, false
	}
	return parseStat(raw)
}

// user names the real user of a process, or gives its uid when the name is unknown.
func (p procFS) user(pid int) string {
	raw, err := p.read(pid, "status")
	if err != nil {
		return ""
	}
	for line := range strings.Lines(raw) {
		v, ok := strings.CutPrefix(line, "Uid:")
		if !ok {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) == 0 {
			return ""
		}
		if u, err := user.LookupId(fields[0]); err == nil {
			return u.Username
		}
		return fields[0]
	}
	return ""
}

// bootTimeMs is the boot time of the host in Unix milliseconds, 0 when unknown.
func (p procFS) bootTimeMs() int64 {
	raw, err := fs.ReadFile(p.fsys, "stat")
	if err != nil {
		return 0
	}
	return parseBootTime(string(raw)) * 1000
}

// descends reports whether process pid is process ancestor or runs below it.
func (p procFS) descends(pid, ancestor int) bool {
	for range 64 {
		if pid == ancestor {
			return true
		}
		st, ok := p.stat(pid)
		if !ok || st.ppid <= 1 {
			return false
		}
		pid = st.ppid
	}
	return false
}

// process describes Kafka process pid that runs with args.
func (p procFS) process(pid int, args []string, bootMs int64) *agentv1.KafkaProcess {
	kp := &agentv1.KafkaProcess{Pid: int64(pid), User: p.user(pid)}
	kp.Version, kp.MixedVersions = kafkaVersion(args)
	if st, ok := p.stat(pid); ok && bootMs > 0 {
		kp.StartTimeMs = bootMs + st.start*1000/userHZ
	}
	return kp
}

// kafkaProcesses finds the Kafka JVMs on the host: the one the agent runs, which is process
// managed or runs below it, and the others. The first result is empty when the agent runs no
// Kafka JVM, also while its start script has not started Java yet. A managed process the agent
// may not read, as on hosts that mount /proc with hidepid, is reported with its pid only.
func (p procFS) kafkaProcesses(managed int) (*agentv1.KafkaProcess, []*agentv1.KafkaProcess) {
	own := &agentv1.KafkaProcess{}
	var foreign []*agentv1.KafkaProcess
	bootMs := int64(-1)
	for _, pid := range p.pids() {
		args := p.args(pid)
		if mainClass(args) != kafkaMainClass {
			continue
		}
		if bootMs < 0 {
			bootMs = p.bootTimeMs()
		}
		kp := p.process(pid, args, bootMs)
		switch {
		case managed <= 0 || !p.descends(pid, managed):
			foreign = append(foreign, kp)
		case own.GetPid() == 0:
			own = kp
		}
	}
	if own.GetPid() == 0 && managed > 0 && p.args(managed) == nil {
		own.Pid = int64(managed)
	}
	return own, foreign
}
