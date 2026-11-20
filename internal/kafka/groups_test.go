// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package kafka

import (
	"context"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestGroupTopicLag(t *testing.T) {
	t.Parallel()
	fake, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(2, "orders"), kfake.SeedTopics(1, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fake.Close)
	cl, err := NewClient(Profile{BootstrapServers: fake.ListenAddrs()}, kgo.RecordPartitioner(kgo.ManualPartitioner()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for topic, counts := range map[string][]int{"orders": {5, 3}, "audit": {4}} {
		for partition, n := range counts {
			for range n {
				if err := cl.KGO.ProduceSync(ctx, &kgo.Record{Topic: topic, Partition: int32(partition), Value: []byte("x")}).FirstErr(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	offsets := kadm.Offsets{}
	offsets.Add(kadm.Offset{Topic: "orders", Partition: 0, At: 2, LeaderEpoch: -1})
	offsets.Add(kadm.Offset{Topic: "orders", Partition: 1, At: 1, LeaderEpoch: -1})
	offsets.Add(kadm.Offset{Topic: "audit", Partition: 0, At: 4, LeaderEpoch: -1})
	if err := cl.Adm.CommitAllOffsets(ctx, "billing", offsets); err != nil {
		t.Fatal(err)
	}
	groups, err := cl.groupSummaries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %+v", groups)
	}
	g := groups[0]
	if g.ID != "billing" || g.Lag != 5 || len(g.TopicLag) != 2 || g.TopicLag["orders"] != 5 || g.TopicLag["audit"] != 0 {
		t.Errorf("group %s: lag %d, lag per topic %v; want 5 with orders 5 and audit 0", g.ID, g.Lag, g.TopicLag)
	}
}
