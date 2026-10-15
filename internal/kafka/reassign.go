// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package kafka

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/twmb/franz-go/pkg/kadm"
)

// Throttle configuration keys (KIP-73).
const (
	LeaderThrottleRate       = "leader.replication.throttled.rate"
	FollowerThrottleRate     = "follower.replication.throttled.rate"
	LeaderThrottledReplicas  = "leader.replication.throttled.replicas"
	FollowerThrottledRepicas = "follower.replication.throttled.replicas"
	CordonedLogDirs          = "cordoned.log.dirs"
)

// Assignment is the target replica list of one partition.
type Assignment struct {
	Topic     string  `json:"topic"`
	Partition int32   `json:"partition"`
	Replicas  []int32 `json:"replicas"`
}

// Key returns "topic-partition".
func (a Assignment) Key() string { return fmt.Sprintf("%s-%d", a.Topic, a.Partition) }

// AlterAssignments starts reassignments; per-partition errors are joined.
func (c *Client) AlterAssignments(ctx context.Context, as []Assignment) error {
	var req kadm.AlterPartitionAssignmentsReq
	for _, a := range as {
		req.Assign(a.Topic, a.Partition, a.Replicas)
	}
	resp, err := c.Adm.AlterPartitionAssignments(ctx, req)
	if err != nil {
		return err
	}
	var errs []error
	resp.Each(func(r kadm.AlterPartitionAssignmentsResponse) {
		if r.Err != nil {
			errs = append(errs, fmt.Errorf("%s-%d: %w", r.Topic, r.Partition, r.Err))
		}
	})
	return errors.Join(errs...)
}

// CancelAssignments cancels in-progress reassignments of the given partitions.
func (c *Client) CancelAssignments(ctx context.Context, as []Assignment) error {
	var req kadm.AlterPartitionAssignmentsReq
	for _, a := range as {
		req.CancelAssign(a.Topic, a.Partition)
	}
	resp, err := c.Adm.AlterPartitionAssignments(ctx, req)
	if err != nil {
		return err
	}
	var errs []error
	resp.Each(func(r kadm.AlterPartitionAssignmentsResponse) {
		if r.Err != nil && !strings.Contains(r.Err.Error(), "NO_REASSIGNMENT_IN_PROGRESS") {
			errs = append(errs, fmt.Errorf("%s-%d: %w", r.Topic, r.Partition, r.Err))
		}
	})
	return errors.Join(errs...)
}

// InProgress returns the keys of partitions that are being reassigned.
func (c *Client) InProgress(ctx context.Context) (map[string]Reassignment, error) {
	resp, err := c.Adm.ListPartitionReassignments(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]Reassignment{}
	resp.Each(func(r kadm.ListPartitionReassignmentsResponse) {
		out[fmt.Sprintf("%s-%d", r.Topic, r.Partition)] = Reassignment{Topic: r.Topic, Partition: r.Partition,
			Replicas: r.Replicas, Adding: r.AddingReplicas, Removing: r.RemovingReplicas}
	})
	return out, nil
}

// CurrentReplicas returns replica lists by "topic-partition" for the given topics.
func (c *Client) CurrentReplicas(ctx context.Context, topics ...string) (map[string][]int32, error) {
	md, err := c.Adm.Metadata(ctx, topics...)
	if err != nil {
		return nil, err
	}
	out := map[string][]int32{}
	for _, td := range md.Topics {
		for _, pd := range td.Partitions {
			out[fmt.Sprintf("%s-%d", td.Topic, pd.Partition)] = pd.Replicas
		}
	}
	return out, nil
}

// ThrottleSpec lists what to throttle for a batch of moves.
type ThrottleSpec struct {
	RateBytesPerSec int64               `json:"rateBytesPerSec"`
	Brokers         []int32             `json:"brokers"`
	LeaderReplicas  map[string][]string `json:"leaderReplicas"`
	FollowerReplica map[string][]string `json:"followerReplicas"`
}

// BuildThrottle computes throttles like kafka-reassign-partitions: leader throttles on current
// replicas, follower throttles on replicas being added, rates on live brokers only.
func BuildThrottle(rate int64, moves []Assignment, current map[string][]int32, alive map[int32]bool) ThrottleSpec {
	spec := ThrottleSpec{RateBytesPerSec: rate, LeaderReplicas: map[string][]string{}, FollowerReplica: map[string][]string{}}
	brokers := map[int32]bool{}
	for _, m := range moves {
		cur := current[m.Key()]
		for _, r := range cur {
			if alive[r] {
				spec.LeaderReplicas[m.Topic] = append(spec.LeaderReplicas[m.Topic], fmt.Sprintf("%d:%d", m.Partition, r))
				brokers[r] = true
			}
		}
		for _, r := range m.Replicas {
			if !containsID(cur, r) && alive[r] {
				spec.FollowerReplica[m.Topic] = append(spec.FollowerReplica[m.Topic], fmt.Sprintf("%d:%d", m.Partition, r))
				brokers[r] = true
			}
		}
	}
	for b := range brokers {
		spec.Brokers = append(spec.Brokers, b)
	}
	sort.Slice(spec.Brokers, func(i, j int) bool { return spec.Brokers[i] < spec.Brokers[j] })
	return spec
}

// ApplyThrottle sets throttle rates on brokers and throttled replica lists on topics.
func (c *Client) ApplyThrottle(ctx context.Context, spec ThrottleSpec) error {
	if spec.RateBytesPerSec <= 0 {
		return nil
	}
	rate := strconv.FormatInt(spec.RateBytesPerSec, 10)
	var errs []error
	for _, b := range spec.Brokers {
		_, err := c.Adm.AlterBrokerConfigs(ctx, []kadm.AlterConfig{
			{Op: kadm.SetConfig, Name: LeaderThrottleRate, Value: &rate},
			{Op: kadm.SetConfig, Name: FollowerThrottleRate, Value: &rate},
		}, b)
		if err != nil {
			errs = append(errs, fmt.Errorf("broker %d: %w", b, err))
		}
	}
	topics := map[string]bool{}
	for t := range spec.LeaderReplicas {
		topics[t] = true
	}
	for t := range spec.FollowerReplica {
		topics[t] = true
	}
	for t := range topics {
		var cfg []kadm.AlterConfig
		if l := strings.Join(spec.LeaderReplicas[t], ","); l != "" {
			cfg = append(cfg, kadm.AlterConfig{Op: kadm.SetConfig, Name: LeaderThrottledReplicas, Value: &l})
		}
		if f := strings.Join(spec.FollowerReplica[t], ","); f != "" {
			cfg = append(cfg, kadm.AlterConfig{Op: kadm.SetConfig, Name: FollowerThrottledRepicas, Value: &f})
		}
		if err := alterTopic(ctx, c, t, cfg); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ClearThrottle removes throttle settings from live brokers and from topics.
func (c *Client) ClearThrottle(ctx context.Context, brokers []int32, topics []string) error {
	var errs []error
	for _, b := range brokers {
		_, err := c.Adm.AlterBrokerConfigs(ctx, []kadm.AlterConfig{
			{Op: kadm.DeleteConfig, Name: LeaderThrottleRate},
			{Op: kadm.DeleteConfig, Name: FollowerThrottleRate},
		}, b)
		if err != nil {
			errs = append(errs, fmt.Errorf("broker %d: %w", b, err))
		}
	}
	for _, t := range topics {
		if err := alterTopic(ctx, c, t, []kadm.AlterConfig{
			{Op: kadm.DeleteConfig, Name: LeaderThrottledReplicas},
			{Op: kadm.DeleteConfig, Name: FollowerThrottledRepicas},
		}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func alterTopic(ctx context.Context, c *Client, topic string, cfg []kadm.AlterConfig) error {
	if len(cfg) == 0 {
		return nil
	}
	resp, err := c.Adm.AlterTopicConfigs(ctx, cfg, topic)
	if err != nil {
		return fmt.Errorf("topic %s: %w", topic, err)
	}
	for _, r := range resp {
		if r.Err != nil {
			return fmt.Errorf("topic %s: %w", topic, r.Err)
		}
	}
	return nil
}

// ElectPreferred runs a preferred leader election for the given partitions (all when empty).
// Partitions whose preferred leader already leads are not errors.
func (c *Client) ElectPreferred(ctx context.Context, partitions []Assignment) error {
	var set kadm.TopicsSet
	if len(partitions) > 0 {
		set = kadm.TopicsSet{}
		for _, p := range partitions {
			set.Add(p.Topic, p.Partition)
		}
	}
	resp, err := c.Adm.ElectLeaders(ctx, kadm.ElectPreferredReplica, set)
	if err != nil {
		return err
	}
	var errs []error
	for topic, parts := range resp {
		for p, r := range parts {
			if r.Err != nil && !strings.Contains(r.Err.Error(), "ELECTION_NOT_NEEDED") {
				errs = append(errs, fmt.Errorf("%s-%d: %w", topic, p, r.Err))
			}
		}
	}
	return errors.Join(errs...)
}

// SetCordon cordons (all log dirs) or uncordons a live broker (KIP-1066).
func (c *Client) SetCordon(ctx context.Context, broker int32, cordon bool) error {
	all := "*"
	cfg := kadm.AlterConfig{Op: kadm.DeleteConfig, Name: CordonedLogDirs}
	if cordon {
		cfg = kadm.AlterConfig{Op: kadm.SetConfig, Name: CordonedLogDirs, Value: &all}
	}
	resp, err := c.Adm.AlterBrokerConfigs(ctx, []kadm.AlterConfig{cfg}, broker)
	if err != nil {
		return fmt.Errorf("broker %d: %w", broker, err)
	}
	for _, r := range resp {
		if r.Err != nil {
			return fmt.Errorf("broker %d: %w", broker, r.Err)
		}
	}
	return nil
}

func containsID(set []int32, x int32) bool {
	for _, v := range set {
		if v == x {
			return true
		}
	}
	return false
}
