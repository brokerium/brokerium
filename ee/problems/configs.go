// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package problems

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
)

// TopicConfigParams are the parameters of alter-topic-configs.
type TopicConfigParams struct {
	Topics []string          `json:"topics"`
	Set    map[string]string `json:"set,omitempty"`
	Delete []string          `json:"delete,omitempty"`
}

func (p TopicConfigParams) alters() []kadm.AlterConfig {
	out := setConfigs(p.Set)
	for _, k := range p.Delete {
		out = append(out, kadm.AlterConfig{Op: kadm.DeleteConfig, Name: k})
	}
	return out
}

func setConfigs(set map[string]string) []kadm.AlterConfig {
	var out []kadm.AlterConfig
	for _, k := range slices.Sorted(maps.Keys(set)) {
		v := set[k]
		out = append(out, kadm.AlterConfig{Op: kadm.SetConfig, Name: k, Value: &v})
	}
	return out
}

func deleteConfigs(keys []string) []kadm.AlterConfig {
	out := make([]kadm.AlterConfig, 0, len(keys))
	for _, k := range keys {
		out = append(out, kadm.AlterConfig{Op: kadm.DeleteConfig, Name: k})
	}
	return out
}

func (p TopicConfigParams) validate() error {
	if len(p.Topics) == 0 {
		return errors.New("topics are required")
	}
	if len(p.Set) == 0 && len(p.Delete) == 0 {
		return errors.New("set or delete at least one config")
	}
	for k := range p.Set {
		if strings.TrimSpace(k) == "" {
			return errors.New("config names must not be empty")
		}
		if slices.Contains(p.Delete, k) {
			return fmt.Errorf("%s is both set and deleted", k)
		}
	}
	for _, k := range p.Delete {
		if strings.TrimSpace(k) == "" {
			return errors.New("config names must not be empty")
		}
	}
	return nil
}

// alterError joins the errors of an IncrementalAlterConfigs response.
func alterError(resp kadm.AlterConfigsResponses, err error) error {
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range resp {
		if r.Err == nil {
			continue
		}
		name := r.Name
		if name == "" {
			name = "cluster-wide"
		}
		if r.ErrMessage != "" {
			errs = append(errs, fmt.Errorf("%s: %w: %s", name, r.Err, r.ErrMessage))
		} else {
			errs = append(errs, fmt.Errorf("%s: %w", name, r.Err))
		}
	}
	return errors.Join(errs...)
}

// configChange is one changed config of a topic in a plan.
type configChange struct {
	Topic string `json:"topic"`
	Key   string `json:"key"`
	From  string `json:"from"`
	To    string `json:"to"`
}

func planAlterTopicConfigs(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := decodeParams[TopicConfigParams](raw)
	if err != nil {
		return nil, err
	}
	if err := params.validate(); err != nil {
		return nil, err
	}
	topics, err := lookupTopics(p.View.Snapshot, params.Topics)
	if err != nil {
		return nil, err
	}
	described, err := p.Client.Adm.DescribeTopicConfigs(ctx, params.Topics...)
	if err != nil {
		return nil, fmt.Errorf("describing the topic configs: %w", err)
	}
	if err := alterError(p.Client.Adm.ValidateAlterTopicConfigs(ctx, params.alters(), params.Topics...)); err != nil {
		return nil, fmt.Errorf("the brokers refuse the change: %w", err)
	}
	current := func(topic, key string) string {
		rc, err := described.On(topic, nil)
		if err != nil {
			return "unknown"
		}
		for _, c := range rc.Configs {
			if c.Key == key {
				if c.Source == kmsg.ConfigSourceDynamicTopicConfig {
					return c.MaybeValue()
				}
				return c.MaybeValue() + " (default)"
			}
		}
		return "not set"
	}
	var changes []configChange
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(params.Set)) {
		parts = append(parts, k+"="+params.Set[k])
		for _, t := range params.Topics {
			changes = append(changes, configChange{Topic: t, Key: k, From: current(t, k), To: params.Set[k]})
		}
	}
	for _, k := range params.Delete {
		parts = append(parts, "remove "+k)
		for _, t := range params.Topics {
			changes = append(changes, configChange{Topic: t, Key: k, From: current(t, k), To: "default"})
		}
	}
	doc := ops.PlanDoc{Summary: fmt.Sprintf("Change the configs of %s: %s", topicList(params.Topics), strings.Join(parts, ", ")),
		Warnings: minISRWarnings(topics, params.Set["min.insync.replicas"]), Details: map[string]any{"changes": changes}}
	return &ops.Planned{Doc: doc, Steps: []ops.StepSpec{{Name: "Change the configs of " + topicList(params.Topics),
		Kind: KindAlterTopicConfigs, Args: params}}}, nil
}

// minISRWarnings warns about a new min.insync.replicas that producers with acks=all cannot meet.
func minISRWarnings(topics []kafka.Topic, value string) []string {
	minISR, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	var out []string
	for _, t := range topics {
		rf := replicationFactor(t)
		switch {
		case minISR > rf:
			out = append(out, fmt.Sprintf("%s: min.insync.replicas %d is above the replication factor %d, so producers with acks=all fail", t.Name, minISR, rf))
			continue
		case minISR == rf && rf > 1:
			out = append(out, fmt.Sprintf("%s: with min.insync.replicas equal to the replication factor %d, producers with acks=all fail while a broker of a partition is down", t.Name, rf))
		}
		short := 0
		for _, p := range t.Partitions {
			if len(p.ISR) < minISR {
				short++
			}
		}
		if short > 0 {
			out = append(out, fmt.Sprintf("%s: %d partition(s) have fewer than %d in-sync replicas now, so producers with acks=all fail on them until the replicas catch up", t.Name, short, minISR))
		}
	}
	return out
}

func stepAlterTopicConfigs(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	var params TopicConfigParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if err := alterError(x.Client.Adm.AlterTopicConfigs(ctx, params.alters(), params.Topics...)); err != nil {
		return nil, err
	}
	return map[string]any{"topics": params.Topics, "set": params.Set, "delete": params.Delete}, nil
}

// AlignParams are the parameters of align-broker-configs.
type AlignParams struct {
	Keys []string `json:"keys"`
	// Values chooses the cluster-wide value of keys; by default the value most brokers have.
	Values map[string]string `json:"values,omitempty"`
}

// perBrokerKey reports configs that differ between brokers by design: ids, listeners, log
// directories, racks and TLS stores.
func perBrokerKey(key string) bool {
	switch key {
	case "broker.id", "node.id", "listeners", "advertised.listeners", "log.dir", "log.dirs", "broker.rack":
		return true
	}
	return strings.HasPrefix(key, "listener.") || strings.Contains(key, "ssl.keystore.") || strings.Contains(key, "ssl.truststore.")
}

// brokerConfig is a config of a broker resource as DescribeConfigs reports it.
type brokerConfig struct {
	Value     string
	Source    kmsg.ConfigSource
	ReadOnly  bool
	Sensitive bool
}

// alignment is the change of one config key.
type alignment struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Values are the values of the serving brokers before the change, by broker id.
	Values map[string]string `json:"values"`
	// Overrides are the brokers whose own value of the key is removed.
	Overrides []int32 `json:"overrides"`
	// ClusterDefault is the cluster-wide value before the change, if one is set.
	ClusterDefault *string `json:"clusterDefault,omitempty"`
}

// aligned reports whether the key already has one cluster-wide value.
func (a alignment) aligned() bool {
	if len(a.Overrides) > 0 || a.ClusterDefault == nil || *a.ClusterDefault != a.Value {
		return false
	}
	for _, v := range a.Values {
		if v != a.Value {
			return false
		}
	}
	return true
}

// align decides the change of every key from the configs of the serving brokers, keyed by
// broker id, and of the cluster-wide resource, keyed by "".
func align(params AlignParams, brokers []int32, configs map[string]map[string]brokerConfig) ([]alignment, error) {
	out := make([]alignment, 0, len(params.Keys))
	for _, key := range params.Keys {
		a := alignment{Key: key, Values: map[string]string{}, Overrides: []int32{}}
		counts := map[string]int{}
		var order []string
		for _, b := range brokers {
			c, ok := configs[strconv.Itoa(int(b))][key]
			switch {
			case !ok:
				return nil, fmt.Errorf("broker %d does not report the config %s", b, key)
			case c.ReadOnly:
				return nil, fmt.Errorf("%s is read-only: brokers read it from server.properties at startup, so it changes only with a rolling restart", key)
			case c.Sensitive:
				return nil, fmt.Errorf("%s is sensitive, and align-broker-configs does not copy secrets between brokers", key)
			}
			a.Values[strconv.Itoa(int(b))] = c.Value
			if counts[c.Value] == 0 {
				order = append(order, c.Value)
			}
			counts[c.Value]++
			if c.Source == kmsg.ConfigSourceDynamicBrokerConfig {
				a.Overrides = append(a.Overrides, b)
			}
		}
		if c, ok := configs[""][key]; ok && c.Source == kmsg.ConfigSourceDynamicDefaultBrokerConfig {
			v := c.Value
			a.ClusterDefault = &v
		}
		if v, ok := params.Values[key]; ok {
			a.Value = v
		} else {
			for _, v := range order {
				if counts[v] > counts[a.Value] {
					a.Value = v
				}
			}
		}
		out = append(out, a)
	}
	return out, nil
}

// describeBrokerConfigs reads keys from the cluster-wide resource and from every given broker.
func describeBrokerConfigs(ctx context.Context, cl *kafka.Client, brokers []int32, keys []string) (map[string]map[string]brokerConfig, error) {
	req := kmsg.NewPtrDescribeConfigsRequest()
	names := []string{""}
	for _, b := range brokers {
		names = append(names, strconv.Itoa(int(b)))
	}
	for _, name := range names {
		rr := kmsg.NewDescribeConfigsRequestResource()
		rr.ResourceType = kmsg.ConfigResourceTypeBroker
		rr.ResourceName = name
		rr.ConfigNames = keys
		req.Resources = append(req.Resources, rr)
	}
	out := map[string]map[string]brokerConfig{}
	for _, shard := range cl.KGO.RequestSharded(ctx, req) {
		if shard.Err != nil {
			return nil, shard.Err
		}
		resp, ok := shard.Resp.(*kmsg.DescribeConfigsResponse)
		if !ok {
			return nil, fmt.Errorf("unexpected response %T", shard.Resp)
		}
		for _, r := range resp.Resources {
			if err := kerr.ErrorForCode(r.ErrorCode); err != nil {
				return nil, fmt.Errorf("broker resource %q: %w", r.ResourceName, err)
			}
			m := map[string]brokerConfig{}
			for _, c := range r.Configs {
				v := ""
				if c.Value != nil {
					v = *c.Value
				}
				m[c.Name] = brokerConfig{Value: v, Source: c.Source, ReadOnly: c.ReadOnly, Sensitive: c.IsSensitive}
			}
			out[r.ResourceName] = m
		}
	}
	return out, nil
}

// clusterConfigArgs set configs on the cluster-wide broker resource.
type clusterConfigArgs struct {
	Set map[string]string `json:"set"`
}

// brokerConfigArgs remove the own values of configs of one broker.
type brokerConfigArgs struct {
	Broker int32    `json:"broker"`
	Delete []string `json:"delete"`
}

func planAlignBrokerConfigs(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := decodeParams[AlignParams](raw)
	if err != nil {
		return nil, err
	}
	if len(params.Keys) == 0 {
		return nil, errors.New("keys are required")
	}
	for i, k := range params.Keys {
		switch {
		case strings.TrimSpace(k) == "":
			return nil, errors.New("config names must not be empty")
		case slices.Contains(params.Keys[:i], k):
			return nil, fmt.Errorf("%s is listed twice", k)
		case perBrokerKey(k):
			return nil, fmt.Errorf("%s differs between brokers by design and has no cluster-wide value", k)
		}
	}
	for k := range params.Values {
		if !slices.Contains(params.Keys, k) {
			return nil, fmt.Errorf("values has %s, which is not one of the keys", k)
		}
	}
	var brokers, stopped []int32
	for _, b := range p.View.Snapshot.Brokers {
		if serving(b) {
			brokers = append(brokers, b.ID)
		} else {
			stopped = append(stopped, b.ID)
		}
	}
	if len(brokers) == 0 {
		return nil, errors.New("no broker serves")
	}
	configs, err := describeBrokerConfigs(ctx, p.Client, brokers, params.Keys)
	if err != nil {
		return nil, fmt.Errorf("describing the broker configs: %w", err)
	}
	aligns, err := align(params, brokers, configs)
	if err != nil {
		return nil, err
	}
	set := map[string]string{}
	deletes := map[int32][]string{}
	for _, a := range aligns {
		if a.aligned() {
			continue
		}
		set[a.Key] = a.Value
		for _, b := range a.Overrides {
			deletes[b] = append(deletes[b], a.Key)
		}
	}
	doc := ops.PlanDoc{Details: map[string]any{"keys": aligns}}
	if len(stopped) > 0 {
		doc.Warnings = append(doc.Warnings, fmt.Sprintf("broker(s) %s do not serve: their own values cannot be read or removed now", joinIDs(stopped)))
	}
	if len(set) == 0 {
		doc.Summary = "The serving brokers already use one cluster-wide value of " + strings.Join(params.Keys, ", ")
		return &ops.Planned{Doc: doc}, nil
	}
	if err := alterError(p.Client.Adm.ValidateAlterBrokerConfigs(ctx, setConfigs(set))); err != nil {
		return nil, fmt.Errorf("the brokers refuse the cluster-wide value: %w", err)
	}
	ids := slices.Sorted(maps.Keys(deletes))
	for _, b := range ids {
		if err := alterError(p.Client.Adm.ValidateAlterBrokerConfigs(ctx, deleteConfigs(deletes[b]), b)); err != nil {
			return nil, fmt.Errorf("broker %d refuses to remove its own values: %w", b, err)
		}
	}
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(set)) {
		parts = append(parts, k+"="+set[k])
	}
	steps := []ops.StepSpec{{Name: "Set " + strings.Join(parts, ", ") + " for the whole cluster", Kind: KindSetClusterConfigs,
		Args: clusterConfigArgs{Set: set}}}
	removed := 0
	for _, b := range ids {
		removed += len(deletes[b])
		steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Remove the own values of broker %d", b), Kind: KindDeleteBrokerConfigs,
			Args: brokerConfigArgs{Broker: b, Delete: deletes[b]}})
	}
	doc.Summary = fmt.Sprintf("Use the cluster-wide value %s and remove %d per-broker value(s) on %d broker(s)",
		strings.Join(parts, ", "), removed, len(ids))
	return &ops.Planned{Doc: doc, Steps: steps}, nil
}

func joinIDs(ids []int32) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.Itoa(int(id))
	}
	return strings.Join(out, ", ")
}

func stepSetClusterConfigs(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	var args clusterConfigArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if err := alterError(x.Client.Adm.AlterBrokerConfigs(ctx, setConfigs(args.Set))); err != nil {
		return nil, err
	}
	return map[string]any{"set": args.Set}, nil
}

func stepDeleteBrokerConfigs(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	var args brokerConfigArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	state, err := x.View(ctx)
	if err != nil {
		return nil, err
	}
	// Changing the config of a stopped broker waits for it to answer.
	if !state.Alive[args.Broker] {
		return nil, fmt.Errorf("broker %d does not serve, so its own values stay", args.Broker)
	}
	if err := alterError(x.Client.Adm.AlterBrokerConfigs(ctx, deleteConfigs(args.Delete), args.Broker)); err != nil {
		return nil, err
	}
	return map[string]any{"broker": args.Broker, "deleted": args.Delete}, nil
}
