// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package messagetools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/store"
)

// CopyParams are the parameters of the copy-messages operation.
type CopyParams struct {
	// Topic is the source topic in the cluster of the operation.
	Topic string `json:"topic"`
	Selection
	Filter
	// TargetCluster is the id of the target cluster; the cluster of the operation by default.
	TargetCluster string `json:"targetCluster,omitempty"`
	TargetTopic   string `json:"targetTopic"`
	// Partitioning is "same" to keep partition numbers or "key" for the default partitioner of
	// the target; "same" by default when the target has enough partitions.
	Partitioning string `json:"partitioning,omitempty"`
	// KeepTimestamps copies the record timestamps (the default); otherwise the target sets them.
	KeepTimestamps *bool `json:"keepTimestamps,omitempty"`
	// ThrottleBytesPerSec limits the copy rate: 10 MiB/s by default.
	ThrottleBytesPerSec int64 `json:"throttleBytesPerSec,omitempty"`
}

// copyArgs are the arguments of one copy-partition step.
type copyArgs struct {
	Topic               string `json:"topic"`
	Range               Range  `json:"range"`
	Filter              Filter `json:"filter"`
	FromTime            *int64 `json:"fromTime,omitempty"`
	ToTime              *int64 `json:"toTime,omitempty"`
	TargetCluster       string `json:"targetCluster"`
	TargetTopic         string `json:"targetTopic"`
	SamePartition       bool   `json:"samePartition"`
	KeepTimestamps      bool   `json:"keepTimestamps"`
	ThrottleBytesPerSec int64  `json:"throttleBytesPerSec"`
}

// copyProgress is the detail of a copy-partition step.
type copyProgress struct {
	Copied  int64 `json:"copied"`
	Scanned int64 `json:"scanned"`
	// Next is the first source offset not copied yet.
	Next int64 `json:"next"`
}

// KindCopyPartition copies a range of one partition.
const KindCopyPartition = "copy-partition"

const defaultCopyThrottle = 10 << 20

// OperationTypes returns the operation types of the message tools.
func OperationTypes() []ops.OpType {
	return []ops.OpType{{
		Type:        "copy-messages",
		Title:       "Copy messages",
		Description: "Copy or replay the records of a topic to a topic of this or another cluster.",
		Scope:       "cluster",
		Requires:    []string{"browseMessages"},
		Feature:     edition.MessageTools,
		Plan:        planCopy,
	}}
}

// StepKinds returns the step kinds of the message tools.
func StepKinds() map[string]ops.StepFunc {
	return map[string]ops.StepFunc{KindCopyPartition: stepCopyPartition}
}

func planCopy(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	var params CopyParams
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if params.Topic == "" || params.TargetTopic == "" {
		return nil, errors.New("topic and targetTopic are required")
	}
	if _, err := params.compile(params.FromTime, params.ToTime); err != nil {
		return nil, err
	}
	ranges, err := Ranges(ctx, p.Client.Adm, params.Topic, params.Selection)
	if err != nil {
		return nil, err
	}
	target := params.TargetCluster
	if target == "" {
		target = p.Cluster.ID
	}
	targetClient, targetCluster := p.Client, p.Cluster
	if target != p.Cluster.ID {
		view, err := p.Engine.Clusters.View(ctx, target, time.Minute)
		if err != nil {
			return nil, fmt.Errorf("target cluster: %w", err)
		}
		if !view.Capabilities.BrowseMessages {
			return nil, errors.New("the target cluster does not allow producing messages")
		}
		if targetClient, targetCluster, err = p.Engine.Clusters.Client(ctx, target); err != nil {
			return nil, fmt.Errorf("target cluster: %w", err)
		}
	}
	topics, err := targetClient.Adm.ListTopics(ctx, params.TargetTopic)
	if err != nil {
		return nil, fmt.Errorf("target topic: %w", err)
	}
	detail, ok := topics[params.TargetTopic]
	if !ok || detail.Err != nil {
		return nil, fmt.Errorf("the topic %s does not exist in cluster %s; create it first", params.TargetTopic, targetCluster.Name)
	}
	targetPartitions := len(detail.Partitions)
	highest := -1
	var records int64
	for _, r := range ranges {
		highest = max(highest, int(r.Partition))
		records += r.To - r.From
	}
	switch params.Partitioning {
	case "":
		params.Partitioning = "key"
		if highest < targetPartitions {
			params.Partitioning = "same"
		}
	case "same":
		if highest >= targetPartitions {
			return nil, fmt.Errorf("partitioning same needs partition %d in %s, which has %d partitions", highest, params.TargetTopic, targetPartitions)
		}
	case "key":
	default:
		return nil, fmt.Errorf("partitioning must be same or key, not %q", params.Partitioning)
	}
	keep := params.KeepTimestamps == nil || *params.KeepTimestamps
	throttle := params.ThrottleBytesPerSec
	if throttle <= 0 {
		throttle = defaultCopyThrottle
	}
	var steps []ops.StepSpec
	for _, r := range ranges {
		if r.From >= r.To {
			continue
		}
		steps = append(steps, ops.StepSpec{
			Name: fmt.Sprintf("Copy partition %d, offsets %d to %d", r.Partition, r.From, r.To-1),
			Kind: KindCopyPartition,
			Args: copyArgs{Topic: params.Topic, Range: r, Filter: params.Filter, FromTime: params.FromTime, ToTime: params.ToTime,
				TargetCluster: targetCluster.ID, TargetTopic: params.TargetTopic, SamePartition: params.Partitioning == "same",
				KeepTimestamps: keep, ThrottleBytesPerSec: throttle},
		})
	}
	if len(steps) == 0 {
		return nil, errors.New("the selection has no records to copy")
	}
	var warnings []string
	if targetCluster.ID == p.Cluster.ID && params.TargetTopic == params.Topic {
		warnings = append(warnings, "the records are appended to the same topic, so its consumers read them again")
	}
	if params.Key != nil || params.Value != nil || params.Header != nil || params.FromTime != nil || params.ToTime != nil {
		warnings = append(warnings, "the record count includes records that the filter skips")
	}
	warnings = append(warnings, "a restart of the server during a step copies the records since its last progress again")
	return &ops.Planned{
		Doc: ops.PlanDoc{
			Summary: fmt.Sprintf("Copy up to %d records of %s to %s in cluster %s (partitioning %s)",
				records, params.Topic, params.TargetTopic, targetCluster.Name, params.Partitioning),
			Warnings: warnings,
			Details: map[string]any{"records": records, "ranges": ranges, "targetCluster": targetCluster.ID,
				"partitioning": params.Partitioning, "throttleBytesPerSec": throttle, "keepTimestamps": keep},
		},
		Steps: steps,
	}, nil
}

// resumeFrom returns the offset where an earlier run of this step stopped, if any.
func resumeFrom(x *ops.Exec, raw json.RawMessage) (copyProgress, bool) {
	for _, st := range x.Op.Steps {
		if st.Kind != KindCopyPartition || !bytes.Equal(st.Args, raw) || st.Status != store.StepRunning || len(st.Detail) == 0 {
			continue
		}
		var p copyProgress
		if json.Unmarshal(st.Detail, &p) == nil && p.Next > 0 {
			return p, true
		}
	}
	return copyProgress{}, false
}

func stepCopyPartition(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	var args copyArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	progress := copyProgress{Next: args.Range.From}
	if earlier, ok := resumeFrom(x, raw); ok {
		progress = earlier
	}
	match, err := args.Filter.compile(args.FromTime, args.ToTime)
	if err != nil {
		return nil, err
	}
	source, err := x.Engine.Clusters.Profile(x.Cluster)
	if err != nil {
		return nil, err
	}
	target := source
	if args.TargetCluster != x.Cluster.ID {
		c, err := x.Engine.Store.GetCluster(ctx, args.TargetCluster)
		if err != nil {
			return nil, fmt.Errorf("target cluster: %w", err)
		}
		if target, err = x.Engine.Clusters.Profile(c); err != nil {
			return nil, err
		}
	}
	err = copyRange(ctx, source, target, args, &progress, match, func() { x.Progress(progress) })
	return progress, err
}

// copyRange copies the records of one range that match, from progress.Next on. Progress
// advances only past records that the target acknowledged.
func copyRange(ctx context.Context, source, target kafka.Profile, args copyArgs, progress *copyProgress,
	match func(*kgo.Record) bool, report func()) error {
	opts := []kgo.Opt{kgo.RequiredAcks(kgo.AllISRAcks())}
	if args.SamePartition {
		opts = append(opts, kgo.RecordPartitioner(kgo.ManualPartitioner()))
	}
	producer, err := kafka.NewClient(target, opts...)
	if err != nil {
		return err
	}
	defer producer.Close()
	var (
		mu         sync.Mutex
		produceErr error
		pending    int64
		lastOffset int64 = -1
		sent       int64
	)
	start := time.Now()
	flush := func() error {
		if err := producer.KGO.Flush(ctx); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		if produceErr != nil {
			return produceErr
		}
		progress.Copied += pending
		pending = 0
		if lastOffset >= 0 {
			progress.Next = lastOffset + 1
		}
		return nil
	}
	r := args.Range
	r.From = max(r.From, progress.Next)
	var scanErr error
	res, err := Scan(ctx, source, args.Topic, []Range{r}, Limits{}, func(rec *kgo.Record) bool {
		progress.Scanned++
		lastOffset = rec.Offset
		if match(rec) {
			out := &kgo.Record{Topic: args.TargetTopic, Key: rec.Key, Value: rec.Value, Headers: rec.Headers}
			if args.SamePartition {
				out.Partition = rec.Partition
			}
			if args.KeepTimestamps {
				out.Timestamp = rec.Timestamp
			}
			mu.Lock()
			pending++
			mu.Unlock()
			producer.KGO.Produce(ctx, out, func(_ *kgo.Record, err error) {
				if err != nil {
					mu.Lock()
					if produceErr == nil {
						produceErr = err
					}
					mu.Unlock()
				}
			})
			sent += int64(len(rec.Key) + len(rec.Value))
		}
		if progress.Scanned%500 == 0 {
			if scanErr = flush(); scanErr != nil {
				return false
			}
			report()
			// Sleep until the bytes sent so far fit the throttle.
			if ahead := time.Duration(float64(sent)/float64(args.ThrottleBytesPerSec)*float64(time.Second)) - time.Since(start); ahead > 0 {
				select {
				case <-ctx.Done():
					return false
				case <-time.After(ahead):
				}
			}
		}
		return true
	})
	if err == nil {
		err = scanErr
	}
	if err == nil {
		err = flush()
	}
	if err == nil && res.Complete {
		progress.Next = args.Range.To
	}
	report()
	return err
}
