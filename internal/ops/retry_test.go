// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestBackoff(t *testing.T) {
	t.Parallel()
	eof := fmt.Errorf("broker 103: %w", io.EOF)
	permanent := errors.New("no brokers to move replicas to")
	tests := []struct {
		name      string
		timeout   time.Duration
		errs      []error
		wantErr   error
		wantCalls int
	}{
		{"succeeds at once", time.Minute, nil, nil, 1},
		{"retries transient errors", time.Minute, []error{eof, eof}, nil, 3},
		{"stops on other errors", time.Minute, []error{eof, permanent}, permanent, 2},
		{"gives up after the timeout", 0, []error{eof, eof, eof}, eof, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := backoff{timeout: tt.timeout, first: time.Millisecond, max: time.Millisecond}
			calls := 0
			err := b.run(context.Background(), &Exec{}, "test", func() error {
				calls++
				if calls <= len(tt.errs) {
					return tt.errs[calls-1]
				}
				return nil
			})
			if !errors.Is(err, tt.wantErr) || calls != tt.wantCalls {
				t.Errorf("run = %v after %d calls, want %v after %d", err, calls, tt.wantErr, tt.wantCalls)
			}
		})
	}
}
