// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	maxLoginFailures = 5
	firstLockout     = 30 * time.Second
	maxLockout       = 15 * time.Minute
	// Every argon2id check allocates 64 MiB; this bounds the memory unauthenticated clients
	// can make the server use.
	concurrentPasswordChecks = 4
	maxTrackedLogins         = 10000
)

// loginLimiter slows down password guessing. After maxLoginFailures failed logins for the same
// user from the same address, further attempts are refused for a period that doubles with
// every failure, up to maxLockout. A successful login resets the counter.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string]*loginFailures
	checks   chan struct{}
	now      func() time.Time
}

type loginFailures struct {
	count int
	last  time.Time
	until time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		failures: map[string]*loginFailures{},
		checks:   make(chan struct{}, concurrentPasswordChecks),
		now:      time.Now,
	}
}

func loginKey(r *http.Request, username string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return username + "\x00" + host
}

// retryAfter returns how long logins for key are refused.
func (l *loginLimiter) retryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if f := l.failures[key]; f != nil {
		if d := f.until.Sub(l.now()); d > 0 {
			return d
		}
	}
	return 0
}

func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	f := l.failures[key]
	if f == nil {
		if len(l.failures) >= maxTrackedLogins {
			l.prune(now)
			if len(l.failures) >= maxTrackedLogins {
				return
			}
		}
		f = &loginFailures{}
		l.failures[key] = f
	}
	f.count++
	f.last = now
	if f.count >= maxLoginFailures {
		d := firstLockout
		for i := maxLoginFailures; i < f.count && d < maxLockout; i++ {
			d *= 2
		}
		f.until = now.Add(min(d, maxLockout))
	}
}

func (l *loginLimiter) succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

// prune forgets keys that are not locked and had no failure for maxLockout. l.mu must be held.
func (l *loginLimiter) prune(now time.Time) {
	for k, f := range l.failures {
		if now.After(f.until) && now.Sub(f.last) > maxLockout {
			delete(l.failures, k)
		}
	}
}

// check runs a password check, at most concurrentPasswordChecks at a time.
func (l *loginLimiter) check(ctx context.Context, verify func() bool) (bool, error) {
	select {
	case l.checks <- struct{}{}:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	defer func() { <-l.checks }()
	return verify(), nil
}
