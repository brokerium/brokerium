// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package ops

import (
	"context"
	"time"

	"github.com/brokerium/brokerium/internal/kafka"
)

// backoff repeats an action on transient errors with growing delays until a timeout.
type backoff struct {
	timeout, first, max time.Duration
}

// transientRetry covers broker listeners that start accepting connections shortly after the
// broker registered.
var transientRetry = backoff{timeout: 2 * time.Minute, first: time.Second, max: 15 * time.Second}

// run calls fn until it succeeds, fails with an error that is not transient, or the timeout passes.
func (b backoff) run(ctx context.Context, x *Exec, what string, fn func() error) error {
	deadline := time.Now().Add(b.timeout)
	delay := b.first
	for {
		err := fn()
		if err == nil || !kafka.IsTransient(err) || time.Now().Add(delay).After(deadline) {
			return err
		}
		x.Progress(map[string]any{"retrying": what, "error": err.Error()})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay = min(2*delay, b.max)
	}
}
