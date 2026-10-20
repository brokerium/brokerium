// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package messagetools

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/brokerium/brokerium/internal/kafka"
)

func TestPlanDelete(t *testing.T) {
	t.Parallel()
	starts := map[int32]int64{0: 10, 1: 0, 2: 5}
	ends := map[int32]int64{0: 100, 1: 0, 2: 50}
	ts := int64(1_700_000_000_000)
	tests := []struct {
		name      string
		req       DeleteRequest
		afterTime map[int32]int64
		want      []DeletePartition
		wantErr   bool
	}{
		{"all", DeleteRequest{All: true}, nil, []DeletePartition{
			{Partition: 0, StartOffset: 10, EndOffset: 100, DeleteBefore: 100, Records: 90},
			{Partition: 2, StartOffset: 5, EndOffset: 50, DeleteBefore: 50, Records: 45},
		}, false},
		{"offsets are clamped", DeleteRequest{Partitions: []PartitionOffset{{0, 40}, {2, 500}, {1, 3}}}, nil, []DeletePartition{
			{Partition: 0, StartOffset: 10, EndOffset: 100, DeleteBefore: 40, Records: 30},
			{Partition: 2, StartOffset: 5, EndOffset: 50, DeleteBefore: 50, Records: 45},
		}, false},
		{"offset before the start", DeleteRequest{Partitions: []PartitionOffset{{0, 5}}}, nil, nil, false},
		{"before a time", DeleteRequest{BeforeTimestamp: &ts}, map[int32]int64{0: 60, 1: 0, 2: -1}, []DeletePartition{
			{Partition: 0, StartOffset: 10, EndOffset: 100, DeleteBefore: 60, Records: 50},
			{Partition: 2, StartOffset: 5, EndOffset: 50, DeleteBefore: 50, Records: 45},
		}, false},
		{"nothing chosen", DeleteRequest{}, nil, nil, true},
		{"two modes", DeleteRequest{All: true, BeforeTimestamp: &ts}, nil, nil, true},
		{"unknown partition", DeleteRequest{Partitions: []PartitionOffset{{7, 1}}}, nil, nil, true},
		{"negative offset", DeleteRequest{Partitions: []PartitionOffset{{0, -1}}}, nil, nil, true},
		{"partition twice", DeleteRequest{Partitions: []PartitionOffset{{0, 20}, {0, 30}}}, nil, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := planDelete(tt.req, starts, ends, tt.afterTime)
			if (err != nil) != tt.wantErr {
				t.Fatalf("planDelete error = %v, want error %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrBadDeleteRequest) {
				t.Fatalf("planDelete error = %v, want ErrBadDeleteRequest", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("planDelete =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestDeleteAudit(t *testing.T) {
	t.Parallel()
	ts := int64(1_700_000_000_000)
	res := &DeleteResult{Records: 30, Failed: 1, Partitions: []DeletePartition{
		{Partition: 0, DeleteBefore: 40, Records: 30},
		{Partition: 1, DeleteBefore: 9, Error: "NOT_LEADER_OR_FOLLOWER"},
	}}
	detail := deleteAudit(DeleteRequest{BeforeTimestamp: &ts}, res)
	before, _ := detail["deleteBefore"].(map[int32]int64)
	if detail["mode"] != "time" || detail["beforeTimestamp"] != ts || detail["records"] != int64(30) || len(before) != 1 || before[0] != 40 {
		t.Fatalf("deleteAudit = %v", detail)
	}
	if err := auditResult(res, nil); err == nil || err.Error() != "1 of 2 partitions failed" {
		t.Fatalf("auditResult = %v", err)
	}
	if err := auditResult(&DeleteResult{}, nil); err != nil {
		t.Fatalf("auditResult without failures = %v", err)
	}
	if detail := deleteAudit(DeleteRequest{All: true}, nil); detail["mode"] != "all" || detail["records"] != nil {
		t.Fatalf("deleteAudit after an error = %v", detail)
	}
}

func TestDeletable(t *testing.T) {
	t.Parallel()
	for policy, want := range map[string]bool{"delete": true, "compact,delete": true, "delete, compact": true, "compact": false, "": false} {
		if got := deletable(policy); got != want {
			t.Errorf("deletable(%q) = %v", policy, got)
		}
	}
}

// fakeKafka starts an in-memory Kafka cluster with the topic "events" of two partitions and
// returns a client that produces to explicit partitions and the profile of the cluster.
func fakeKafka(t *testing.T) (*kgo.Client, *kadm.Client, kafka.Profile) {
	t.Helper()
	c, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(2, "events"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	cl, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl.Close)
	return cl, kadm.NewClient(cl), kafka.Profile{BootstrapServers: c.ListenAddrs()}
}

func fakeCluster(t *testing.T) (*kgo.Client, *kadm.Client) {
	t.Helper()
	cl, adm, _ := fakeKafka(t)
	return cl, adm
}

// produceRecords writes n records with timestamps one second apart from base.
func produceRecords(t *testing.T, cl *kgo.Client, topic string, partition int32, n int, base time.Time) {
	t.Helper()
	for i := range n {
		rec := &kgo.Record{Topic: topic, Partition: partition, Key: []byte("k"), Value: []byte("v"),
			Timestamp: base.Add(time.Duration(i) * time.Second)}
		if err := cl.ProduceSync(context.Background(), rec).FirstErr(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeleteRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cl, adm := fakeCluster(t)
	base := time.UnixMilli(1_700_000_000_000)
	produceRecords(t, cl, "events", 0, 10, base)
	produceRecords(t, cl, "events", 1, 5, base)

	dry, err := DeleteRecords(ctx, adm, "events", DeleteRequest{All: true, DryRun: true})
	if err != nil || dry.Records != 15 || len(dry.Partitions) != 2 {
		t.Fatalf("dry run = %+v, %v", dry, err)
	}
	if starts, _ := listed(adm.ListStartOffsets(ctx, "events")); starts[0] != 0 || starts[1] != 0 {
		t.Fatalf("a dry run deleted records: start offsets %v", starts)
	}

	res, err := DeleteRecords(ctx, adm, "events", DeleteRequest{Partitions: []PartitionOffset{{0, 4}}})
	if err != nil || res.Records != 4 || res.Failed != 0 || res.Partitions[0].DeleteBefore != 4 {
		t.Fatalf("delete before offset 4 = %+v, %v", res, err)
	}

	before := base.Add(7 * time.Second).UnixMilli()
	res, err = DeleteRecords(ctx, adm, "events", DeleteRequest{BeforeTimestamp: &before})
	if err != nil || res.Records != 8 {
		t.Fatalf("delete before the eighth second = %+v, %v", res, err)
	}

	res, err = DeleteRecords(ctx, adm, "events", DeleteRequest{All: true})
	if err != nil || res.Records != 3 {
		t.Fatalf("empty the topic = %+v, %v", res, err)
	}
	starts, _ := listed(adm.ListStartOffsets(ctx, "events"))
	ends, _ := listed(adm.ListEndOffsets(ctx, "events"))
	if starts[0] != ends[0] || starts[1] != ends[1] {
		t.Fatalf("after emptying: start offsets %v, end offsets %v", starts, ends)
	}
}

func TestDeleteRecordsRefusesCompactedTopics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, adm := fakeCluster(t)
	compact := "compact"
	if _, err := adm.CreateTopic(ctx, 1, 1, map[string]*string{"cleanup.policy": &compact}, "keys"); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteRecords(ctx, adm, "keys", DeleteRequest{All: true, DryRun: true}); !errors.Is(err, ErrCompactedTopic) {
		t.Fatalf("DeleteRecords on a compacted topic = %v, want ErrCompactedTopic", err)
	}
	if _, err := DeleteRecords(ctx, adm, "missing", DeleteRequest{All: true, DryRun: true}); err == nil {
		t.Fatal("DeleteRecords on a missing topic succeeded")
	}
}
