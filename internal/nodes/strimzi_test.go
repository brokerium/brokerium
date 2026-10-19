// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
)

func TestBrokersServing(t *testing.T) {
	t.Parallel()
	dirs := []kafka.LogDir{{Path: "/var/lib/kafka"}}
	serving := kafka.Broker{ID: 100, Alive: true, LogDirs: dirs}
	reachable := func(context.Context, int32) error { return nil }
	tests := []struct {
		name    string
		brokers []kafka.Broker
		ping    func(context.Context, int32) error
		want    bool
		wantErr bool
	}{
		{"serving", []kafka.Broker{serving, {ID: 103, Alive: true, LogDirs: dirs}}, reachable, true, false},
		{"not registered", []kafka.Broker{serving}, reachable, false, false},
		{"fenced", []kafka.Broker{serving, {ID: 103, Alive: true, Fenced: true, LogDirs: dirs}}, reachable, false, false},
		{"no log dirs yet", []kafka.Broker{serving, {ID: 103, Alive: true}}, reachable, false, false},
		{"log dirs not reported by the cluster", []kafka.Broker{{ID: 100, Alive: true}, {ID: 103, Alive: true}}, reachable, true, false},
		{"listener not reachable", []kafka.Broker{serving, {ID: 103, Alive: true, LogDirs: dirs}},
			func(_ context.Context, id int32) error { return fmt.Errorf("broker %d: %w", id, io.EOF) }, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ok, _, err := brokersServing(context.Background(), &kafka.Snapshot{Brokers: tt.brokers}, []int32{100, 103}, tt.ping)
			if ok != tt.want || (err != nil) != tt.wantErr {
				t.Errorf("brokersServing = %v, %v; want %v, error %v", ok, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestWaitForUnreachableBroker(t *testing.T) {
	t.Parallel()
	snap := &kafka.Snapshot{Brokers: []kafka.Broker{{ID: 103, Alive: true}}}
	pings := 0
	ping := func(context.Context, int32) error {
		if pings++; pings < 3 {
			return io.EOF
		}
		return nil
	}
	_, err := poll(context.Background(), &ops.Exec{}, time.Second, time.Millisecond, "node pool brokers",
		func(ctx context.Context) (bool, any, error) { return brokersServing(ctx, snap, []int32{103}, ping) })
	if err != nil || pings != 3 {
		t.Fatalf("poll = %v after %d pings, want success after 3", err, pings)
	}
}
