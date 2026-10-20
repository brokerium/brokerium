// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package docker

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func dialError(errno syscall.Errno) error {
	return &url.Error{Op: "Get", URL: "http://docker/_ping", Err: &net.OpError{Op: "dial", Net: "unix",
		Addr: &net.UnixAddr{Name: "/var/run/docker.sock", Net: "unix"}, Err: os.NewSyscallError("connect", errno)}}
}

func TestDetector(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		mode          string
		dockerenv     bool
		kubernetes    string
		ping          error
		wantAvailable bool
		wantPings     int
		wantReason    []string
	}{
		{name: "on outside a container", mode: ModeOn, ping: errors.New("down"), wantAvailable: true},
		{name: "on in Kubernetes", mode: ModeOn, kubernetes: "10.96.0.1", wantAvailable: true},
		{name: "off in the Docker installation", mode: ModeOff, dockerenv: true,
			wantReason: []string{"turned off", "BROKERIUM_DOCKER_PROVIDER=off"}},
		{name: "Docker installation", mode: ModeAuto, dockerenv: true, wantAvailable: true, wantPings: 1},
		{name: "binary on the host", mode: ModeAuto,
			wantReason: []string{needInstallation, "does not run in a Docker container", "BROKERIUM_DOCKER_PROVIDER=on"}},
		{name: "Kubernetes", mode: ModeAuto, kubernetes: "10.96.0.1", wantReason: []string{needInstallation, "runs in Kubernetes"}},
		{name: "Kubernetes on a Docker runtime", mode: ModeAuto, dockerenv: true, kubernetes: "10.96.0.1",
			wantReason: []string{"runs in Kubernetes"}},
		{name: "socket without access", mode: ModeAuto, dockerenv: true, ping: dialError(syscall.EACCES), wantPings: 1,
			wantReason: []string{needInstallation + "the server cannot reach the Docker API at unix:///var/run/docker.sock: " +
				"dial unix /var/run/docker.sock: connect: permission denied", "--group-add"}},
		{name: "socket not mounted", mode: ModeAuto, dockerenv: true, ping: dialError(syscall.ENOENT), wantPings: 1,
			wantReason: []string{"connect: no such file or directory", "mount the Docker socket"}},
		{name: "daemon does not answer", mode: ModeAuto, dockerenv: true, ping: context.DeadlineExceeded, wantPings: 1,
			wantReason: []string{"the Docker API at unix:///var/run/docker.sock does not answer within 2s"}},
		{name: "daemon refuses", mode: ModeAuto, dockerenv: true, ping: errors.New("docker GET /_ping: HTTP 500: boom"), wantPings: 1,
			wantReason: []string{"HTTP 500: boom"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pings := 0
			d := newDetector(tt.mode, "unix:///var/run/docker.sock", environment{
				fileExists: func(name string) bool { return tt.dockerenv && name == "/.dockerenv" },
				getenv: func(key string) string {
					if key == "KUBERNETES_SERVICE_HOST" {
						return tt.kubernetes
					}
					return ""
				},
				ping: func(context.Context) error { pings++; return tt.ping },
				now:  time.Now,
			})
			got := d.Check(t.Context())
			if got.Available != tt.wantAvailable || pings != tt.wantPings {
				t.Fatalf("Check = %+v after %d ping(s), want available %v after %d", got, pings, tt.wantAvailable, tt.wantPings)
			}
			if tt.wantAvailable != (got.Reason == "") {
				t.Errorf("reason %q", got.Reason)
			}
			for _, part := range tt.wantReason {
				if !strings.Contains(got.Reason, part) {
					t.Errorf("reason %q does not contain %q", got.Reason, part)
				}
			}
			if d.InContainer() != tt.dockerenv {
				t.Errorf("InContainer = %v, want %v", d.InContainer(), tt.dockerenv)
			}
		})
	}
}

func TestDetectorRechecks(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	var pingErr error
	pings := 0
	d := newDetector(ModeAuto, "unix:///var/run/docker.sock", environment{
		fileExists: func(string) bool { return true },
		getenv:     func(string) string { return "" },
		ping: func(ctx context.Context) error {
			pings++
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return pingErr
		},
		now: func() time.Time { return now },
	})
	steps := []struct {
		after         time.Duration
		pingErr       error
		wantAvailable bool
		wantPings     int
	}{
		{0, dialError(syscall.ENOENT), false, 1},
		{10 * time.Second, nil, false, 1},
		{21 * time.Second, nil, true, 2},
		{29 * time.Second, dialError(syscall.ENOENT), true, 2},
		{time.Second, dialError(syscall.ENOENT), false, 3},
	}
	for i, s := range steps {
		now = now.Add(s.after)
		pingErr = s.pingErr
		if got := d.Check(t.Context()); got.Available != s.wantAvailable || pings != s.wantPings {
			t.Fatalf("step %d: Check = %+v after %d ping(s), want available %v after %d", i, got, pings, s.wantAvailable, s.wantPings)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	now = now.Add(recheckAfter)
	if got := d.Check(ctx); got.Available || !strings.Contains(got.Reason, "no such file") {
		t.Errorf("Check with an ended request = %+v, want the result of the ping", got)
	}
}

func TestNilDetector(t *testing.T) {
	t.Parallel()
	var d *Detector
	if a := d.Check(t.Context()); a.Available || a.Reason == "" {
		t.Errorf("Check = %+v", a)
	}
	if d.InContainer() || d.Client() != nil {
		t.Error("a missing detector has no container and no client")
	}
}

func TestAvailabilityErr(t *testing.T) {
	t.Parallel()
	if err := (Availability{Available: true}).Err(); err != nil {
		t.Errorf("Err of an available provider = %v", err)
	}
	reason := needInstallation + "this server runs in Kubernetes"
	err := Availability{Reason: reason}.Err()
	if !errors.Is(err, ErrUnavailable) || err.Error() != reason {
		t.Errorf("Err = %v, want %q matching ErrUnavailable", err, reason)
	}
}
