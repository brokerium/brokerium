// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package docker

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSplitImage(t *testing.T) {
	t.Parallel()
	tests := []struct{ image, repo, tag string }{
		{"bvaliev/brokerium-agent:lab", "bvaliev/brokerium-agent", "lab"},
		{"bvaliev/brokerium-agent", "bvaliev/brokerium-agent", "latest"},
		{"registry.example.com:5000/brokerium-agent", "registry.example.com:5000/brokerium-agent", "latest"},
		{"registry.example.com:5000/brokerium-agent:0.3.0-lab", "registry.example.com:5000/brokerium-agent", "0.3.0-lab"},
		{"bvaliev/brokerium-agent@sha256:0123abcd", "bvaliev/brokerium-agent", "sha256:0123abcd"},
	}
	for _, tt := range tests {
		if repo, tag := splitImage(tt.image); repo != tt.repo || tag != tt.tag {
			t.Errorf("splitImage(%q) = %q, %q, want %q, %q", tt.image, repo, tag, tt.repo, tt.tag)
		}
	}
}

// fakeEngine answers the Docker Engine API requests of Run for an image that is not pulled yet.
type fakeEngine struct {
	pullError string

	mu     sync.Mutex
	pulled []string
}

func (f *fakeEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/json"):
		http.Error(w, `{"message":"No such container: lab-c1"}`, http.StatusNotFound)
	case r.URL.Path == "/volumes/create":
		w.WriteHeader(http.StatusCreated)
	case r.URL.Path == "/containers/create" && len(f.pulled) == 0:
		http.Error(w, `{"message":"No such image: bvaliev/brokerium-agent:lab"}`, http.StatusNotFound)
	case r.URL.Path == "/containers/create":
		_, _ = fmt.Fprint(w, `{"Id":"c0ffee"}`)
	case r.URL.Path == "/images/create":
		_, _ = fmt.Fprintln(w, `{"status":"Pulling from bvaliev/brokerium-agent","id":"lab"}`)
		if f.pullError != "" {
			_, _ = fmt.Fprintf(w, `{"errorDetail":{"message":%q},"error":%q}`+"\n", f.pullError, f.pullError)
			return
		}
		f.pulled = append(f.pulled, r.URL.Query().Get("fromImage")+":"+r.URL.Query().Get("tag"))
		_, _ = fmt.Fprintln(w, `{"status":"Status: Downloaded newer image for bvaliev/brokerium-agent:lab"}`)
	case strings.HasSuffix(r.URL.Path, "/start"):
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusTeapot)
	}
}

func TestRunPullsAMissingImage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, pullError, wantErr string
		wantPulled               []string
	}{
		{name: "pulled", wantPulled: []string{"bvaliev/brokerium-agent:lab"}},
		{name: "pull fails", pullError: "manifest for bvaliev/brokerium-agent:lab not found", wantErr: "manifest for"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			engine := &fakeEngine{pullError: tt.pullError}
			srv := httptest.NewServer(engine)
			defer srv.Close()
			c, err := New("tcp://" + srv.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			id, err := c.Run(t.Context(), ContainerSpec{Name: "lab-c1", Image: "bvaliev/brokerium-agent:lab",
				Volumes: map[string]string{"lab-c1-data": "/var/lib/kafka/data"}})
			engine.mu.Lock()
			defer engine.mu.Unlock()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Run = %q, %v, want an error with %q", id, err, tt.wantErr)
				}
				return
			}
			if err != nil || id != "c0ffee" || strings.Join(engine.pulled, ",") != strings.Join(tt.wantPulled, ",") {
				t.Errorf("Run = %q, %v after pulling %v, want c0ffee after pulling %v", id, err, engine.pulled, tt.wantPulled)
			}
		})
	}
}

// TestSocketThatAppearsLater checks that a client keeps working with a socket the Docker daemon
// creates after the server started.
func TestSocketThatAppearsLater(t *testing.T) {
	t.Parallel()
	// Unix socket paths are limited to about 100 bytes, which a test's temporary directory can exceed.
	dir, err := os.MkdirTemp("", "brk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "docker.sock")
	c := unixClient("unix://"+path, path)
	if err := c.Ping(t.Context()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Ping without a socket = %v, want a missing file", err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("OK")) })}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	if err := c.Ping(t.Context()); err != nil {
		t.Fatalf("Ping after the socket appeared = %v", err)
	}
	if c.Host() != "unix://"+path {
		t.Errorf("Host = %q", c.Host())
	}
}
