// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// kafkaArgs is the command line kafka-run-class.sh starts a broker with.
func kafkaArgs(classpath string) []string {
	return []string{"/usr/lib/jvm/java-21/bin/java", "-Xmx1G", "-Xms1G", "-server", "-XX:+UseG1GC",
		"-Dkafka.logs.dir=/var/lib/brokerium-agent/logs", "-cp", classpath, "kafka.Kafka", "/var/lib/brokerium-agent/server.properties"}
}

func jars(dir string, names ...string) string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, dir+"/"+n)
	}
	return strings.Join(out, ":")
}

func TestMainClass(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"broker", kafkaArgs(jars("/opt/kafka/libs", "kafka-clients-4.3.1.jar")), "kafka.Kafka"},
		{"options with values", []string{"java", "--add-opens", "java.base/java.lang=ALL-UNNAMED", "-classpath", "kafka.Kafka",
			"org.example.Main"}, "org.example.Main"},
		{"storage tool", []string{"java", "-cp", "/opt/kafka/libs/*", "kafka.tools.StorageTool", "format"}, "kafka.tools.StorageTool"},
		{"start script before it runs java", []string{"/bin/bash", "/opt/kafka/bin/kafka-server-start.sh", "/etc/kafka/server.properties"},
			"/opt/kafka/bin/kafka-server-start.sh"},
		{"jar", []string{"java", "-jar", "app.jar", "kafka.Kafka"}, ""},
		{"no arguments", nil, ""},
	}
	for _, tt := range tests {
		if got := mainClass(tt.args); got != tt.want {
			t.Errorf("%s: mainClass = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestKafkaVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		version string
		mixed   bool
	}{
		{"release", kafkaArgs(jars("/opt/brokerium/kafka/current/bin/../libs", "kafka-clients-4.4.0.jar", "kafka-server-4.4.0.jar",
			"kafka_2.13-4.4.0.jar")), "4.4.0", false},
		{"core jar only", kafkaArgs(jars("/opt/kafka/libs", "kafka_2.13-3.9.1.jar")), "3.9.1", false},
		{"old jars before the release", kafkaArgs(jars("/opt/kafka-3.9/libs", "kafka-clients-3.9.1.jar") + ":" +
			jars("/opt/kafka/libs", "kafka-clients-4.4.0.jar", "kafka_2.13-4.4.0.jar")), "3.9.1", true},
		{"clients jar after an old core jar", kafkaArgs(jars("/opt/kafka/libs", "kafka_2.13-4.3.1.jar", "kafka-clients-4.4.0.jar")),
			"4.4.0", true},
		{"test and source jars", kafkaArgs(jars("/opt/kafka/libs", "kafka-clients-4.4.0-test.jar", "kafka_2.13-4.4.0-sources.jar",
			"kafka-clients-4.3.1.jar")), "4.3.1", false},
		{"wildcard", kafkaArgs("/opt/kafka/libs/*"), "", false},
		{"class path option", []string{"java", "--class-path=/opt/kafka/libs/kafka-clients-4.3.1.jar", "kafka.Kafka"}, "4.3.1", false},
		{"no class path", []string{"java", "kafka.Kafka"}, "", false},
	}
	for _, tt := range tests {
		version, mixed := kafkaVersion(tt.args)
		if version != tt.version || mixed != tt.mixed {
			t.Errorf("%s: kafkaVersion = %q, %v; want %q, %v", tt.name, version, mixed, tt.version, tt.mixed)
		}
	}
}

// statLine is /proc/<pid>/stat of a process with the command name comm.
func statLine(pid int, comm string, ppid int, start int64) string {
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[1], fields[19] = "S", fmt.Sprint(ppid), fmt.Sprint(start)
	return fmt.Sprintf("%d (%s) %s\n", pid, comm, strings.Join(fields, " "))
}

func TestParseStat(t *testing.T) {
	t.Parallel()
	st, ok := parseStat(statLine(4242, "java (main) x", 17, 123456))
	if !ok || st.ppid != 17 || st.start != 123456 {
		t.Errorf("parseStat = %+v, %v; want ppid 17, start 123456", st, ok)
	}
	for _, raw := range []string{"", "4242 java S 1", "4242 (java) S x 0", "4242 (java) S 1 2 3"} {
		if _, ok := parseStat(raw); ok {
			t.Errorf("parseStat(%q) succeeded", raw)
		}
	}
	if got := parseBootTime("cpu  1 2 3\nbtime 1763625600\nprocesses 42\n"); got != 1763625600 {
		t.Errorf("parseBootTime = %d", got)
	}
}

// fakeProc is a proc file system with the boot time and the processes added to it.
type fakeProc struct {
	fs fstest.MapFS
}

func newFakeProc(boot int64) fakeProc {
	return fakeProc{fs: fstest.MapFS{"stat": {Data: fmt.Appendf(nil, "cpu 1 2 3\nbtime %d\n", boot)}}}
}

// add adds a process with its parent, its start time in clock ticks after boot, its uid and its
// command line.
func (f fakeProc) add(pid, ppid int, start int64, uid string, args ...string) {
	dir := fmt.Sprint(pid) + "/"
	f.fs[dir+"cmdline"] = &fstest.MapFile{Data: []byte(strings.Join(args, "\x00") + "\x00")}
	f.fs[dir+"stat"] = &fstest.MapFile{Data: []byte(statLine(pid, "java", ppid, start))}
	f.fs[dir+"status"] = &fstest.MapFile{Data: []byte("Name:\tjava\nUid:\t" + uid + "\t" + uid + "\t" + uid + "\t" + uid + "\n")}
}

func TestKafkaProcesses(t *testing.T) {
	t.Parallel()
	const boot = 1763625600
	release := kafkaArgs(jars("/opt/brokerium/kafka/current/bin/../libs", "kafka-clients-4.4.0.jar", "kafka_2.13-4.4.0.jar"))
	operator := kafkaArgs(jars("/opt/kafka/libs", "kafka-clients-4.3.1.jar"))
	script := []string{"/bin/bash", "/opt/brokerium/kafka/current/bin/kafka-server-start.sh", "/var/lib/brokerium-agent/server.properties"}
	p := newFakeProc(boot)
	p.add(1, 0, 1, "0", "/sbin/init")
	p.add(200, 1, 5000, "54321", release...)
	p.add(300, 1, 900, "54322", operator...)
	p.add(350, 1, 7000, "54321", script...)
	p.add(400, 350, 7010, "54321", release...)
	p.add(500, 1, 100, "0", "java", "-cp", "/opt/kafka/libs/*", "kafka.tools.StorageTool", "info")
	// A process that ended between listing /proc and reading it.
	p.fs["600/stat"] = &fstest.MapFile{Data: []byte(statLine(600, "java", 1, 1))}
	p.add(700, 1, 8000, "54321", script...)
	tests := []struct {
		name    string
		managed int
		own     int64
		foreign []int64
	}{
		{"main process of the unit", 200, 200, []int64{300, 400}},
		{"Java below the start script", 350, 400, []int64{200, 300}},
		{"start script before it runs Java", 700, 0, []int64{200, 300, 400}},
		{"process hidden from the agent", 800, 800, []int64{200, 300, 400}},
		{"no process of the agent", 0, 0, []int64{200, 300, 400}},
	}
	for _, tt := range tests {
		own, foreign := procFS{p.fs}.kafkaProcesses(tt.managed)
		var got []int64
		for _, f := range foreign {
			got = append(got, f.GetPid())
		}
		slices.Sort(got)
		if own.GetPid() != tt.own || !slices.Equal(got, tt.foreign) {
			t.Errorf("%s: own %d, foreign %v; want %d, %v", tt.name, own.GetPid(), got, tt.own, tt.foreign)
		}
	}
	own, foreign := procFS{p.fs}.kafkaProcesses(200)
	if own.GetVersion() != "4.4.0" || own.GetMixedVersions() || own.GetStartTimeMs() != boot*1000+50_000 || own.GetUser() != "54321" {
		t.Errorf("process of the agent %+v", own)
	}
	if f := foreign[0]; f.GetPid() != 300 || f.GetVersion() != "4.3.1" || f.GetUser() != "54322" || f.GetStartTimeMs() != boot*1000+9_000 {
		t.Errorf("foreign process %+v", f)
	}
	if own, foreign := (procFS{fstest.MapFS{}}).kafkaProcesses(200); own.GetPid() != 200 || own.GetVersion() != "" || len(foreign) != 0 {
		t.Errorf("without /proc: own %+v, foreign %v; want pid 200 only", own, foreign)
	}
}
