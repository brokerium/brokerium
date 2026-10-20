// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package messagetools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/brokerium/brokerium/internal/kafka"
)

// Range is the part of a partition to read: offsets from From up to To, exclusive.
type Range struct {
	Partition int32 `json:"partition"`
	From      int64 `json:"from"`
	To        int64 `json:"to"`
}

// Selection picks the partitions and the time range of a topic to read.
type Selection struct {
	// Partitions to read; all when empty.
	Partitions []int32 `json:"partitions,omitempty"`
	// FromTime and ToTime bound the record timestamps in Unix milliseconds, ToTime exclusive.
	FromTime *int64 `json:"fromTime,omitempty"`
	ToTime   *int64 `json:"toTime,omitempty"`
	// Resume continues an earlier scan from these offsets.
	Resume []PartitionOffset `json:"resume,omitempty"`
}

// Limits bound a scan; zero values mean no limit.
type Limits struct {
	Records int64
	Time    time.Duration
}

// ScanResult says how far a scan got.
type ScanResult struct {
	Scanned int64 `json:"scanned"`
	// Next lists the first offset not read in each partition, to continue a stopped scan.
	Next []PartitionOffset `json:"next"`
	// Complete is set when every range was read to its end.
	Complete bool `json:"complete"`
	// Stopped names what ended an incomplete scan: "limit" (enough results), "records" or
	// "time".
	Stopped string `json:"stopped,omitempty"`
}

// ErrBadSelection is returned for partitions that the topic does not have.
var ErrBadSelection = errors.New("invalid selection")

// idleTimeout ends a scan when no record arrives for this long: compaction and transaction
// markers can leave offsets at the end of a range that no record fills.
const idleTimeout = 5 * time.Second

// Ranges turns a selection into ranges against the current offsets of the topic. The time range
// narrows the offsets; Filter still checks each timestamp, which producers may set out of order.
func Ranges(ctx context.Context, adm *kadm.Client, topic string, sel Selection) ([]Range, error) {
	starts, err := listed(adm.ListStartOffsets(ctx, topic))
	if err != nil {
		return nil, err
	}
	ends, err := listed(adm.ListEndOffsets(ctx, topic))
	if err != nil {
		return nil, err
	}
	afterTime := func(ms *int64) (map[int32]int64, error) {
		if ms == nil {
			return nil, nil
		}
		return listed(adm.ListOffsetsAfterMilli(ctx, *ms, topic))
	}
	froms, err := afterTime(sel.FromTime)
	if err != nil {
		return nil, err
	}
	tos, err := afterTime(sel.ToTime)
	if err != nil {
		return nil, err
	}
	parts := sel.Partitions
	if len(parts) == 0 {
		for p := range ends {
			parts = append(parts, p)
		}
	}
	resume := map[int32]int64{}
	for _, po := range sel.Resume {
		resume[po.Partition] = po.Offset
	}
	var ranges []Range
	for _, p := range parts {
		end, ok := ends[p]
		if !ok {
			return nil, fmt.Errorf("%w: partition %d does not exist", ErrBadSelection, p)
		}
		r := Range{Partition: p, From: starts[p], To: end}
		if at, ok := froms[p]; ok && at >= 0 {
			r.From = max(r.From, at)
		}
		if at, ok := tos[p]; ok && at >= 0 {
			r.To = min(r.To, at)
		}
		if at, ok := resume[p]; ok {
			r.From = max(r.From, at)
		}
		ranges = append(ranges, r)
	}
	slices.SortFunc(ranges, func(a, b Range) int { return int(a.Partition - b.Partition) })
	return ranges, nil
}

// Scan reads the ranges of a topic and calls fn for each record, in offset order within every
// partition; fn returns false to stop. Only committed records of transactions are read.
func Scan(ctx context.Context, profile kafka.Profile, topic string, ranges []Range, limits Limits,
	fn func(*kgo.Record) bool) (res ScanResult, err error) {
	next := map[int32]int64{}
	pending := map[int32]int64{}
	offsets := map[int32]kgo.Offset{}
	for _, r := range ranges {
		next[r.Partition] = r.From
		if r.From < r.To {
			pending[r.Partition] = r.To
			offsets[r.Partition] = kgo.NewOffset().At(r.From)
		}
	}
	defer func() { res.Next = positions(next) }()
	if len(pending) == 0 {
		res.Complete = true
		return res, nil
	}
	cl, err := kafka.NewClient(profile, kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: offsets}),
		kgo.FetchIsolationLevel(kgo.ReadCommitted()), kgo.FetchMaxWait(500*time.Millisecond),
		kgo.FetchMaxBytes(8<<20), kgo.FetchMaxPartitionBytes(1<<20))
	if err != nil {
		return res, err
	}
	defer cl.Close()
	if limits.Time > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limits.Time)
		defer cancel()
	}
	for len(pending) > 0 && res.Stopped == "" {
		pctx, pcancel := context.WithTimeout(ctx, idleTimeout)
		fetches := cl.KGO.PollFetches(pctx)
		idle := pctx.Err() != nil
		pcancel()
		if ctx.Err() != nil {
			if limits.Time > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				res.Stopped = "time"
				break
			}
			return res, ctx.Err()
		}
		var fetchErr error
		fetches.EachError(func(_ string, p int32, err error) {
			if fetchErr == nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				fetchErr = fmt.Errorf("partition %d: %w", p, err)
			}
		})
		if fetchErr != nil {
			return res, fetchErr
		}
		got := 0
		fetches.EachRecord(func(rec *kgo.Record) {
			to, ok := pending[rec.Partition]
			if !ok || res.Stopped != "" {
				return
			}
			if rec.Offset >= to {
				delete(pending, rec.Partition)
				next[rec.Partition] = to
				return
			}
			got++
			res.Scanned++
			next[rec.Partition] = rec.Offset + 1
			if rec.Offset+1 >= to {
				delete(pending, rec.Partition)
			}
			switch {
			case !fn(rec):
				res.Stopped = "limit"
			case limits.Records > 0 && res.Scanned >= limits.Records && len(pending) > 0:
				res.Stopped = "records"
			}
		})
		if idle && got == 0 {
			for p, to := range pending {
				next[p] = to
			}
			clear(pending)
		}
	}
	res.Complete = len(pending) == 0 && res.Stopped == ""
	return res, nil
}

func positions(next map[int32]int64) []PartitionOffset {
	out := make([]PartitionOffset, 0, len(next))
	for p, o := range next {
		out = append(out, PartitionOffset{Partition: p, Offset: o})
	}
	slices.SortFunc(out, func(a, b PartitionOffset) int { return int(a.Partition - b.Partition) })
	return out
}
