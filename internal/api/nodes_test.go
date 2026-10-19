// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/store"
)

func TestCertificateState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	tests := []struct {
		notAfter *time.Time
		want     string
	}{
		{nil, ""},
		{at(100 * 24 * time.Hour), "ok"},
		{at(30 * 24 * time.Hour), "expiring"},
		{at(time.Minute), "expiring"},
		{at(0), "expired"},
		{at(-time.Hour), "expired"},
	}
	for _, tt := range tests {
		if got := certificateState(&store.Node{CertNotAfter: tt.notAfter}, now, 30*24*time.Hour); got != tt.want {
			t.Errorf("certificateState(%v) = %q, want %q", tt.notAfter, got, tt.want)
		}
	}
}
