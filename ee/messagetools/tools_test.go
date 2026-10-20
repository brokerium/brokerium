// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package messagetools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestFilter(t *testing.T) {
	t.Parallel()
	base := time.UnixMilli(1_700_000_000_000)
	rec := &kgo.Record{Key: []byte("Order-42"), Value: []byte(`{"status":"PAID"}`), Timestamp: base,
		Headers: []kgo.RecordHeader{{Key: "trace", Value: []byte("abc-123")}}}
	at := func(ms int64) *int64 { return &ms }
	tests := []struct {
		name     string
		filter   Filter
		from, to *int64
		want     bool
	}{
		{"empty", Filter{}, nil, nil, true},
		{"key contains", Filter{Key: &Match{Contains: "der-4"}}, nil, nil, true},
		{"key contains is case sensitive", Filter{Key: &Match{Contains: "order"}}, nil, nil, false},
		{"ignore case", Filter{Key: &Match{Contains: "order"}, IgnoreCase: true}, nil, nil, true},
		{"value regex", Filter{Value: &Match{Regex: `"status":"(PAID|SHIPPED)"`}}, nil, nil, true},
		{"value regex misses", Filter{Value: &Match{Regex: `^\[`}}, nil, nil, false},
		{"header key", Filter{Header: &HeaderMatch{Key: "trace"}}, nil, nil, true},
		{"header value", Filter{Header: &HeaderMatch{Key: "trace", Value: &Match{Contains: "123"}}}, nil, nil, true},
		{"other header", Filter{Header: &HeaderMatch{Key: "span"}}, nil, nil, false},
		{"inside the time range", Filter{}, at(base.UnixMilli()), at(base.UnixMilli() + 1), true},
		{"before the time range", Filter{}, at(base.UnixMilli() + 1), nil, false},
		{"end is exclusive", Filter{}, nil, at(base.UnixMilli()), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			match, err := tt.filter.compile(tt.from, tt.to)
			if err != nil {
				t.Fatal(err)
			}
			if got := match(rec); got != tt.want {
				t.Fatalf("match = %v, want %v", got, tt.want)
			}
		})
	}
	for name, f := range map[string]Filter{
		"contains and regex":  {Key: &Match{Contains: "a", Regex: "b"}},
		"invalid regex":       {Value: &Match{Regex: "("}},
		"header without key":  {Header: &HeaderMatch{Value: &Match{Contains: "x"}}},
		"pattern too long":    {Value: &Match{Contains: strings.Repeat("x", maxPattern+1)}},
		"invalid regex value": {Header: &HeaderMatch{Key: "h", Value: &Match{Regex: "[z-a]"}}},
	} {
		if _, err := f.compile(nil, nil); !errors.Is(err, ErrBadFilter) {
			t.Errorf("%s: compile = %v, want ErrBadFilter", name, err)
		}
	}
}

// produceOrders writes n records "order-<i>" with a header to a partition, one second apart.
func produceOrders(t *testing.T, cl *kgo.Client, topic string, partition int32, n int, base time.Time) {
	t.Helper()
	for i := range n {
		rec := &kgo.Record{Topic: topic, Partition: partition, Key: fmt.Appendf(nil, "k%d", i),
			Value: fmt.Appendf(nil, "order-%d", i), Timestamp: base.Add(time.Duration(i) * time.Second),
			Headers: []kgo.RecordHeader{{Key: "parity", Value: []byte([]string{"even", "odd"}[i%2])}}}
		if err := cl.ProduceSync(context.Background(), rec).FirstErr(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cl, adm, profile := fakeKafka(t)
	base := time.UnixMilli(1_700_000_000_000)
	produceOrders(t, cl, "events", 0, 20, base)
	produceOrders(t, cl, "events", 1, 10, base)

	res, err := Search(ctx, adm, profile, "events", SearchRequest{Filter: Filter{Header: &HeaderMatch{Key: "parity", Value: &Match{Contains: "odd"}}}})
	if err != nil || len(res.Messages) != 15 || !res.Complete || res.Scanned != 30 {
		t.Fatalf("search odd = %d matches, %+v, %v", len(res.Messages), res.ScanResult, err)
	}
	if m := res.Messages[0]; m.Partition != 0 || m.Offset != 1 || *m.Value != "order-1" {
		t.Fatalf("first match = %+v", m)
	}

	first, err := Search(ctx, adm, profile, "events", SearchRequest{Selection: Selection{Partitions: []int32{0}}, Limit: 3})
	if err != nil || len(first.Messages) != 3 || first.Complete || first.Stopped != "limit" {
		t.Fatalf("limited search = %d, %+v, %v", len(first.Messages), first.ScanResult, err)
	}
	rest, err := Search(ctx, adm, profile, "events", SearchRequest{Selection: Selection{Partitions: []int32{0}, Resume: first.Next}, Limit: 100})
	if err != nil || len(rest.Messages) != 17 || rest.Messages[0].Offset != 3 || !rest.Complete {
		t.Fatalf("resumed search = %d, %+v, %v", len(rest.Messages), rest.ScanResult, err)
	}

	from, to := base.Add(5*time.Second).UnixMilli(), base.Add(8*time.Second).UnixMilli()
	timed, err := Search(ctx, adm, profile, "events", SearchRequest{Selection: Selection{FromTime: &from, ToTime: &to}})
	if err != nil || len(timed.Messages) != 6 {
		t.Fatalf("search between seconds 5 and 8 = %d, %v", len(timed.Messages), err)
	}

	capped, err := Search(ctx, adm, profile, "events", SearchRequest{MaxRecords: 4, Filter: Filter{Value: &Match{Contains: "nothing"}}})
	if err != nil || capped.Stopped != "records" || capped.Scanned != 4 {
		t.Fatalf("capped search = %+v, %v", capped.ScanResult, err)
	}

	if _, err := Search(ctx, adm, profile, "events", SearchRequest{Selection: Selection{Partitions: []int32{9}}}); !errors.Is(err, ErrBadSelection) {
		t.Fatalf("search of a missing partition = %v", err)
	}
}

func TestExportFormats(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	records := []*kgo.Record{
		{Partition: 0, Offset: 7, Timestamp: ts, Key: []byte("k"), Value: []byte("a,\"b\"\nc"),
			Headers: []kgo.RecordHeader{{Key: "bin", Value: []byte{0xff}}}},
		{Partition: 1, Offset: 8, Timestamp: ts, Value: nil},
	}
	render := func(name string) string {
		t.Helper()
		f, err := lookupFormat(name)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		w := f.open(&buf)
		for _, r := range records {
			if err := w.write(r); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.close(); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	var lines []exportRecord
	sc := bufio.NewScanner(strings.NewReader(render("")))
	for sc.Scan() {
		var r exportRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, r)
	}
	if len(lines) != 2 || *lines[0].Value != "a,\"b\"\nc" || lines[0].Headers[0].Encoding != "base64" || lines[1].Value != nil {
		t.Fatalf("ndjson = %+v", lines)
	}

	var array []exportRecord
	if err := json.Unmarshal([]byte(render("json")), &array); err != nil || len(array) != 2 || array[1].Offset != 8 {
		t.Fatalf("json = %+v, %v", array, err)
	}

	rows, err := csv.NewReader(strings.NewReader(render("csv"))).ReadAll()
	if err != nil || len(rows) != 3 || rows[1][5] != "a,\"b\"\nc" || rows[2][6] != "null" || rows[0][0] != "partition" {
		t.Fatalf("csv = %q, %v", rows, err)
	}

	f, _ := lookupFormat("json")
	var empty bytes.Buffer
	if err := f.open(&empty).close(); err != nil || strings.TrimSpace(empty.String()) != "[]" {
		t.Fatalf("empty json = %q, %v", empty.String(), err)
	}
	if _, err := lookupFormat("xml"); !errors.Is(err, ErrBadFilter) {
		t.Fatalf("lookupFormat(xml) = %v", err)
	}
}

func TestCopyRange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cl, adm, profile := fakeKafka(t)
	if _, err := adm.CreateTopic(ctx, 2, 1, nil, "copy"); err != nil {
		t.Fatal(err)
	}
	base := time.UnixMilli(1_700_000_000_000)
	produceOrders(t, cl, "events", 1, 10, base)
	match, err := Filter{Header: &HeaderMatch{Key: "parity", Value: &Match{Contains: "even"}}}.compile(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	args := copyArgs{Topic: "events", Range: Range{Partition: 1, From: 0, To: 10}, TargetTopic: "copy", SamePartition: true,
		KeepTimestamps: true, ThrottleBytesPerSec: 1 << 30}

	// A run that stopped after offset 3 resumes from offset 4.
	progress := copyProgress{Next: 4}
	reports := 0
	if err := copyRange(ctx, profile, profile, args, &progress, match, func() { reports++ }); err != nil {
		t.Fatal(err)
	}
	if progress.Copied != 3 || progress.Next != 10 || progress.Scanned != 6 || reports == 0 {
		t.Fatalf("progress = %+v after %d reports", progress, reports)
	}

	ranges, err := Ranges(ctx, adm, "copy", Selection{})
	if err != nil {
		t.Fatal(err)
	}
	var copied []string
	var stamps []int64
	if _, err := Scan(ctx, profile, "copy", ranges, Limits{}, func(rec *kgo.Record) bool {
		if rec.Partition != 1 {
			t.Errorf("record copied to partition %d", rec.Partition)
		}
		copied = append(copied, string(rec.Value))
		stamps = append(stamps, rec.Timestamp.UnixMilli())
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(copied, []string{"order-4", "order-6", "order-8"}) || stamps[0] != base.Add(4*time.Second).UnixMilli() {
		t.Fatalf("copied %v with timestamps %v", copied, stamps)
	}
}

func TestScanStopsAtGaps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cl, _, profile := fakeKafka(t)
	produceOrders(t, cl, "events", 0, 3, time.Now())
	start := time.Now()
	// The range claims two offsets past the last record, as compaction can leave them.
	res, err := Scan(ctx, profile, "events", []Range{{Partition: 0, From: 0, To: 5}}, Limits{}, func(*kgo.Record) bool { return true })
	if err != nil || res.Scanned != 3 || !res.Complete || res.Next[0].Offset != 5 {
		t.Fatalf("scan = %+v, %v", res, err)
	}
	if took := time.Since(start); took > idleTimeout+5*time.Second {
		t.Fatalf("scan took %s", took)
	}
}
