// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

// Package messagetools implements the message tools of Brokerium Enterprise: deleting records
// and emptying topics, searching, exporting and copying messages.
package messagetools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/twmb/franz-go/pkg/kadm"
)

// DeleteRequest says which records of a topic to delete; exactly one of Partitions,
// BeforeTimestamp and All is set.
type DeleteRequest struct {
	// Partitions lists an offset per partition; the records before it are deleted.
	Partitions []PartitionOffset `json:"partitions,omitempty"`
	// BeforeTimestamp deletes the records older than this time (Unix milliseconds) in every
	// partition.
	BeforeTimestamp *int64 `json:"beforeTimestamp,omitempty"`
	// All deletes every record, which empties the topic.
	All    bool `json:"all,omitempty"`
	DryRun bool `json:"dryRun"`
	// Confirm repeats the topic name; required unless DryRun is set.
	Confirm string `json:"confirm"`
}

// PartitionOffset is an offset in one partition.
type PartitionOffset struct {
	Partition int32 `json:"partition"`
	Offset    int64 `json:"offset"`
}

// DeletePartition is the deletion in one partition.
type DeletePartition struct {
	Partition   int32 `json:"partition"`
	StartOffset int64 `json:"startOffset"`
	EndOffset   int64 `json:"endOffset"`
	// DeleteBefore becomes the start offset of the partition.
	DeleteBefore int64  `json:"deleteBefore"`
	Records      int64  `json:"records"`
	Error        string `json:"error,omitempty"`
}

// DeleteResult describes a planned or performed deletion.
type DeleteResult struct {
	Partitions []DeletePartition `json:"partitions"`
	Records    int64             `json:"records"`
	Failed     int               `json:"failed"`
	DryRun     bool              `json:"dryRun"`
}

var (
	// ErrBadDeleteRequest is returned for requests that do not say what to delete.
	ErrBadDeleteRequest = errors.New("invalid delete request")
	// ErrCompactedTopic is returned for topics whose cleanup policy does not include delete,
	// where Kafka refuses to delete records.
	ErrCompactedTopic = errors.New("Kafka deletes records only in topics whose cleanup.policy includes delete; " +
		"produce tombstones to remove keys from a compacted topic")
)

// planDelete computes the deletion in each partition from the log start and end offsets and,
// for a time, the first offsets at or after it. Offsets past the end are clamped to the end;
// partitions with nothing to delete are left out.
func planDelete(req DeleteRequest, starts, ends, afterTime map[int32]int64) ([]DeletePartition, error) {
	modes := 0
	for _, set := range []bool{len(req.Partitions) > 0, req.BeforeTimestamp != nil, req.All} {
		if set {
			modes++
		}
	}
	if modes != 1 {
		return nil, fmt.Errorf("%w: set exactly one of partitions, beforeTimestamp and all", ErrBadDeleteRequest)
	}
	targets := map[int32]int64{}
	switch {
	case req.All:
		for p, end := range ends {
			targets[p] = end
		}
	case req.BeforeTimestamp != nil:
		for p, end := range ends {
			if at, ok := afterTime[p]; ok && at >= 0 {
				targets[p] = at
			} else {
				targets[p] = end
			}
		}
	default:
		for _, po := range req.Partitions {
			if _, ok := ends[po.Partition]; !ok {
				return nil, fmt.Errorf("%w: partition %d does not exist", ErrBadDeleteRequest, po.Partition)
			}
			if po.Offset < 0 {
				return nil, fmt.Errorf("%w: offset %d of partition %d is negative", ErrBadDeleteRequest, po.Offset, po.Partition)
			}
			if _, dup := targets[po.Partition]; dup {
				return nil, fmt.Errorf("%w: partition %d is listed twice", ErrBadDeleteRequest, po.Partition)
			}
			targets[po.Partition] = po.Offset
		}
	}
	var plan []DeletePartition
	for p, target := range targets {
		start, end := starts[p], ends[p]
		before := min(max(target, start), end)
		if before > start {
			plan = append(plan, DeletePartition{Partition: p, StartOffset: start, EndOffset: end, DeleteBefore: before, Records: before - start})
		}
	}
	slices.SortFunc(plan, func(a, b DeletePartition) int { return int(a.Partition - b.Partition) })
	return plan, nil
}

// deletable reports whether Kafka deletes records of a topic with this cleanup policy.
func deletable(policy string) bool {
	for p := range strings.SplitSeq(policy, ",") {
		if strings.TrimSpace(p) == "delete" {
			return true
		}
	}
	return false
}

// DeleteRecords plans a deletion against the current offsets of the topic and performs it
// unless the request is a dry run.
func DeleteRecords(ctx context.Context, adm *kadm.Client, topic string, req DeleteRequest) (*DeleteResult, error) {
	configs, err := adm.DescribeTopicConfigs(ctx, topic)
	if err != nil {
		return nil, err
	}
	rc, err := configs.On(topic, nil)
	if err != nil {
		return nil, err
	}
	policy := "delete"
	for _, c := range rc.Configs {
		if c.Key == "cleanup.policy" && c.Value != nil {
			policy = *c.Value
		}
	}
	if !deletable(policy) {
		return nil, ErrCompactedTopic
	}
	starts, err := listed(adm.ListStartOffsets(ctx, topic))
	if err != nil {
		return nil, err
	}
	ends, err := listed(adm.ListEndOffsets(ctx, topic))
	if err != nil {
		return nil, err
	}
	var afterTime map[int32]int64
	if req.BeforeTimestamp != nil {
		if afterTime, err = listed(adm.ListOffsetsAfterMilli(ctx, *req.BeforeTimestamp, topic)); err != nil {
			return nil, err
		}
	}
	plan, err := planDelete(req, starts, ends, afterTime)
	if err != nil {
		return nil, err
	}
	result := &DeleteResult{Partitions: plan, DryRun: req.DryRun}
	if req.DryRun || len(plan) == 0 {
		for _, p := range plan {
			result.Records += p.Records
		}
		return result, nil
	}
	offsets := kadm.Offsets{}
	for _, p := range plan {
		offsets.Add(kadm.Offset{Topic: topic, Partition: p.Partition, At: p.DeleteBefore})
	}
	resps, err := adm.DeleteRecords(ctx, offsets)
	if err != nil {
		return nil, err
	}
	for i := range result.Partitions {
		p := &result.Partitions[i]
		resp, ok := resps.Lookup(topic, p.Partition)
		switch {
		case !ok:
			p.Error = "no response from the partition leader"
		case resp.Err != nil:
			p.Error = resp.Err.Error()
		default:
			p.DeleteBefore = resp.LowWatermark
			p.Records = resp.LowWatermark - p.StartOffset
		}
		if p.Error != "" {
			p.Records = 0
			result.Failed++
		}
		result.Records += p.Records
	}
	return result, nil
}

// listed turns listed offsets of one topic into offsets per partition.
func listed(offsets kadm.ListedOffsets, err error) (map[int32]int64, error) {
	if err != nil {
		return nil, err
	}
	out := map[int32]int64{}
	var firstErr error
	offsets.Each(func(o kadm.ListedOffset) {
		switch {
		case o.Err != nil && firstErr == nil:
			firstErr = fmt.Errorf("partition %d: %w", o.Partition, o.Err)
		case o.Err == nil:
			out[o.Partition] = o.Offset
		}
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}
