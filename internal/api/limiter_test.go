// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginLimiterLockout(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	l := newLoginLimiter()
	l.now = func() time.Time { return now }
	key := "admin\x00192.0.2.1"

	for i := 1; i < maxLoginFailures; i++ {
		l.fail(key)
		if d := l.retryAfter(key); d != 0 {
			t.Fatalf("locked for %v after %d failures", d, i)
		}
	}
	steps := []struct {
		name string
		want time.Duration
	}{
		{"first lockout", firstLockout},
		{"doubles", 2 * firstLockout},
		{"doubles again", 4 * firstLockout},
	}
	for _, s := range steps {
		l.fail(key)
		if d := l.retryAfter(key); d != s.want {
			t.Fatalf("%s: retry after %v, want %v", s.name, d, s.want)
		}
	}
	for range 20 {
		l.fail(key)
	}
	if d := l.retryAfter(key); d != maxLockout {
		t.Fatalf("retry after %v, want the cap %v", d, maxLockout)
	}
	if d := l.retryAfter("admin\x00192.0.2.2"); d != 0 {
		t.Errorf("another address is locked for %v", d)
	}

	now = now.Add(maxLockout + time.Second)
	if d := l.retryAfter(key); d != 0 {
		t.Errorf("still locked for %v after the lockout", d)
	}
	l.succeed(key)
	l.fail(key)
	if d := l.retryAfter(key); d != 0 {
		t.Errorf("a successful login did not reset the counter: locked for %v", d)
	}
}

func TestLoginLimiterBoundsPasswordChecks(t *testing.T) {
	t.Parallel()
	l := newLoginLimiter()
	var running, peak atomic.Int32
	var wg sync.WaitGroup
	for range 3 * concurrentPasswordChecks {
		wg.Go(func() {
			_, _ = l.check(context.Background(), func() bool {
				n := running.Add(1)
				for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
				}
				time.Sleep(20 * time.Millisecond)
				running.Add(-1)
				return true
			})
		})
	}
	wg.Wait()
	if p := peak.Load(); p > concurrentPasswordChecks {
		t.Errorf("%d concurrent password checks, want at most %d", p, concurrentPasswordChecks)
	}

	for range concurrentPasswordChecks {
		l.checks <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.check(ctx, func() bool { return true }); err == nil {
		t.Error("a check with a canceled request ran while all slots were busy")
	}
}
