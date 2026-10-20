// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadStall(t *testing.T) {
	t.Parallel()
	const stall = 200 * time.Millisecond
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
		wantErr error
	}{
		{"stalls after some data", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("partial"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}, "", errStalled},
		{"stalls before the response", func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}, "", errStalled},
		{"slow but steady", func(w http.ResponseWriter, _ *http.Request) {
			for range 6 {
				_, _ = w.Write([]byte("ab"))
				w.(http.Flusher).Flush()
				time.Sleep(stall / 2)
			}
		}, "abababababab", nil},
		{"not found", func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		}, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			dest := filepath.Join(t.TempDir(), "archive")
			start := time.Now()
			err := download(context.Background(), srv.URL+"/kafka.tgz", dest, stall)
			if took := time.Since(start); took > 10*stall {
				t.Errorf("download took %s", took)
			}
			switch {
			case tt.want != "":
				got, rerr := os.ReadFile(dest)
				if err != nil || rerr != nil || string(got) != tt.want {
					t.Fatalf("download = %v, file %q (%v)", err, got, rerr)
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("download = %v, want %v", err, tt.wantErr)
				}
			default:
				if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
					t.Fatalf("download = %v, want HTTP 404", err)
				}
			}
		})
	}
}

func TestFetchSHA512NextMirrorAfterStall(t *testing.T) {
	t.Parallel()
	stalled := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer stalled.Close()
	sum := strings.Repeat("ab", 64)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("kafka_2.13-4.4.0.tgz: " + strings.ToUpper(sum)))
	}))
	defer good.Close()
	got, err := fetchSHA512(context.Background(), []string{stalled.URL, good.URL}, 200*time.Millisecond)
	if err != nil || got != sum {
		t.Fatalf("fetchSHA512 = %q, %v", got, err)
	}
}
