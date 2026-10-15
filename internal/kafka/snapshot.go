// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package kafka

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
)

// Snapshot is a point-in-time view of a cluster collected over the Kafka protocol.
type Snapshot struct {
	CollectedAt   time.Time      `json:"collectedAt"`
	DurationMs    int64          `json:"durationMs"`
	ClusterID     string         `json:"clusterId"`
	Brokers       []Broker       `json:"brokers"`
	Quorum        *Quorum        `json:"quorum,omitempty"`
	Features      []Feature      `json:"features"`
	Topics        []Topic        `json:"topics"`
	Reassignments []Reassignment `json:"reassignments"`
	Groups        []Group        `json:"groups"`
	Errors        []string       `json:"errors,omitempty"`
}

// Broker combines registration, metadata and log dir information of one broker.
type Broker struct {
	ID               int32    `json:"id"`
	Host             string   `json:"host"`
	Port             int32    `json:"port"`
	Rack             string   `json:"rack"`
	Fenced           bool     `json:"fenced"`
	Alive            bool     `json:"alive"`
	Cordoned         bool     `json:"cordoned"`
	LogDirs          []LogDir `json:"logDirs"`
	DiskTotalBytes   int64    `json:"diskTotalBytes"`
	DiskUsableBytes  int64    `json:"diskUsableBytes"`
	DataBytes        int64    `json:"dataBytes"`
	Replicas         int      `json:"replicas"`
	Leaders          int      `json:"leaders"`
	PreferredLeaders int      `json:"preferredLeaders"`
}

// LogDir is one log directory of a broker.
type LogDir struct {
	Path        string `json:"path"`
	TotalBytes  int64  `json:"totalBytes"`
	UsableBytes int64  `json:"usableBytes"`
	DataBytes   int64  `json:"dataBytes"`
	Cordoned    bool   `json:"cordoned"`
	Error       string `json:"error,omitempty"`
}

// Topic is a topic with its partitions and key configs.
type Topic struct {
	Name              string            `json:"name"`
	ID                string            `json:"id"`
	Internal          bool              `json:"internal"`
	ReplicationFactor int               `json:"replicationFactor"`
	MinISR            int               `json:"minInsyncReplicas"`
	Configs           map[string]string `json:"configs"`
	SizeBytes         int64             `json:"sizeBytes"`
	Partitions        []Partition       `json:"partitions"`
	Error             string            `json:"error,omitempty"`
}

// Partition is one partition of a topic.
type Partition struct {
	ID           int32           `json:"id"`
	Leader       int32           `json:"leader"`
	LeaderEpoch  int32           `json:"leaderEpoch"`
	Replicas     []int32         `json:"replicas"`
	ISR          []int32         `json:"isr"`
	Offline      []int32         `json:"offline,omitempty"`
	EndOffset    int64           `json:"endOffset"`
	SizeBytes    int64           `json:"sizeBytes"`
	ReplicaBytes map[int32]int64 `json:"replicaBytes,omitempty"`
	Error        string          `json:"error,omitempty"`
}

// Reassignment is an in-progress partition reassignment.
type Reassignment struct {
	Topic     string  `json:"topic"`
	Partition int32   `json:"partition"`
	Replicas  []int32 `json:"replicas"`
	Adding    []int32 `json:"adding"`
	Removing  []int32 `json:"removing"`
}

// Group types.
const (
	GroupTypeConsumer = "consumer"
	GroupTypeShare    = "share"
)

// Group is a consumer or share group summary.
type Group struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	State        string   `json:"state"`
	ProtocolType string   `json:"protocolType"`
	Members      int      `json:"members"`
	Lag          int64    `json:"lag"`
	Topics       []string `json:"topics"`
	Error        string   `json:"error,omitempty"`
}

// SnapshotOptions controls optional, more expensive parts of a snapshot.
type SnapshotOptions struct {
	Groups    bool
	MaxGroups int
}

var snapshotConfigKeys = []string{
	"min.insync.replicas", "retention.ms", "retention.bytes", "cleanup.policy", "segment.bytes",
	"leader.replication.throttled.replicas", "follower.replication.throttled.replicas", "remote.storage.enable",
}

// Snapshot collects brokers, quorum, features, topics, log dirs, reassignments and groups.
// Metadata failure makes the snapshot fail; other failures are recorded in Errors.
func (c *Client) Snapshot(ctx context.Context, opts SnapshotOptions) (*Snapshot, error) {
	start := time.Now()
	md, err := c.Adm.Metadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}
	s := &Snapshot{CollectedAt: start, ClusterID: md.Cluster}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		info     *ClusterInfo
		logDirs  kadm.DescribedAllLogDirs
		configs  kadm.ResourceConfigs
		reassign kadm.ListPartitionReassignmentsResponses
		groups   []Group
		ends     kadm.ListedOffsets
	)
	run := func(name string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				s.Errors = append(s.Errors, fmt.Sprintf("%s: %v", name, err))
				mu.Unlock()
			}
		}()
	}
	topicNames := md.Topics.Names()
	run("describe cluster", func() (err error) { info, err = c.DescribeCluster(ctx, true); return })
	run("describe quorum", func() (err error) { s.Quorum, err = c.DescribeQuorum(ctx); return })
	run("features", func() (err error) { s.Features, err = c.Features(ctx); return })
	run("log dirs", func() (err error) { logDirs, err = c.Adm.DescribeAllLogDirs(ctx, nil); return })
	run("topic configs", func() (err error) {
		if len(topicNames) == 0 {
			return nil
		}
		configs, err = c.Adm.DescribeTopicConfigs(ctx, topicNames...)
		return
	})
	run("reassignments", func() (err error) { reassign, err = c.Adm.ListPartitionReassignments(ctx, nil); return })
	run("end offsets", func() (err error) {
		if len(topicNames) == 0 {
			return nil
		}
		ends, err = c.Adm.ListEndOffsets(ctx, topicNames...)
		return
	})
	if opts.Groups {
		run("groups", func() (err error) { groups, err = c.groupSummaries(ctx, opts.MaxGroups); return })
	}
	wg.Wait()

	// Brokers: registrations (including fenced) merged with live metadata.
	brokers := map[int32]*Broker{}
	if info != nil {
		for _, b := range info.Brokers {
			brokers[b.NodeID] = &Broker{ID: b.NodeID, Host: b.Host, Port: b.Port, Rack: b.Rack, Fenced: b.Fenced}
		}
	}
	for _, b := range md.Brokers {
		br, ok := brokers[b.NodeID]
		if !ok {
			br = &Broker{ID: b.NodeID, Host: b.Host, Port: b.Port}
			brokers[b.NodeID] = br
		}
		br.Alive = true
		if b.Rack != nil {
			br.Rack = *b.Rack
		}
	}

	// Log dirs: disk usage and replica sizes.
	replicaBytes := map[string]map[int32]map[int32]int64{}
	for brokerID, dirs := range logDirs {
		br, ok := brokers[brokerID]
		if !ok {
			continue
		}
		for _, d := range dirs {
			ld := LogDir{Path: d.Dir, TotalBytes: d.TotalBytes, UsableBytes: d.UsableBytes, Cordoned: d.IsCordoned}
			if d.Err != nil {
				ld.Error = d.Err.Error()
			}
			d.Topics.Each(func(p kadm.DescribedLogDirPartition) {
				if p.IsFuture {
					return
				}
				ld.DataBytes += p.Size
				if replicaBytes[p.Topic] == nil {
					replicaBytes[p.Topic] = map[int32]map[int32]int64{}
				}
				if replicaBytes[p.Topic][p.Partition] == nil {
					replicaBytes[p.Topic][p.Partition] = map[int32]int64{}
				}
				replicaBytes[p.Topic][p.Partition][brokerID] = p.Size
			})
			br.LogDirs = append(br.LogDirs, ld)
			if ld.TotalBytes > 0 {
				br.DiskTotalBytes += ld.TotalBytes
				br.DiskUsableBytes += ld.UsableBytes
			}
			br.DataBytes += ld.DataBytes
		}
		br.Cordoned = len(br.LogDirs) > 0
		for _, ld := range br.LogDirs {
			br.Cordoned = br.Cordoned && ld.Cordoned
		}
		sort.Slice(br.LogDirs, func(i, j int) bool { return br.LogDirs[i].Path < br.LogDirs[j].Path })
	}

	// Topic configs.
	topicConfigs := map[string]map[string]string{}
	for _, rc := range configs {
		m := map[string]string{}
		for _, cfg := range rc.Configs {
			for _, k := range snapshotConfigKeys {
				if cfg.Key == k && cfg.Value != nil {
					m[k] = *cfg.Value
				}
			}
		}
		topicConfigs[rc.Name] = m
	}

	// Topics and partitions.
	for _, name := range topicNames {
		td := md.Topics[name]
		t := Topic{Name: name, ID: td.ID.String(), Internal: td.IsInternal, Configs: topicConfigs[name]}
		if td.Err != nil {
			t.Error = td.Err.Error()
		}
		if t.Configs == nil {
			t.Configs = map[string]string{}
		}
		if v, err := strconv.Atoi(t.Configs["min.insync.replicas"]); err == nil {
			t.MinISR = v
		} else {
			t.MinISR = 1
		}
		for _, pd := range td.Partitions.Sorted() {
			p := Partition{ID: pd.Partition, Leader: pd.Leader, LeaderEpoch: pd.LeaderEpoch, Replicas: pd.Replicas,
				ISR: pd.ISR, Offline: pd.OfflineReplicas, ReplicaBytes: replicaBytes[name][pd.Partition]}
			if pd.Err != nil {
				p.Error = pd.Err.Error()
			}
			if o, ok := ends.Lookup(name, pd.Partition); ok && o.Err == nil {
				p.EndOffset = o.Offset
			}
			if size, ok := p.ReplicaBytes[p.Leader]; ok {
				p.SizeBytes = size
			} else {
				for _, size := range p.ReplicaBytes {
					p.SizeBytes = max(p.SizeBytes, size)
				}
			}
			t.SizeBytes += p.SizeBytes
			if len(p.Replicas) > t.ReplicationFactor {
				t.ReplicationFactor = len(p.Replicas)
			}
			for i, r := range p.Replicas {
				if br, ok := brokers[r]; ok {
					br.Replicas++
					if i == 0 {
						br.PreferredLeaders++
					}
				}
			}
			if br, ok := brokers[p.Leader]; ok && p.Leader >= 0 {
				br.Leaders++
			}
			t.Partitions = append(t.Partitions, p)
		}
		s.Topics = append(s.Topics, t)
	}

	for _, br := range brokers {
		s.Brokers = append(s.Brokers, *br)
	}
	sort.Slice(s.Brokers, func(i, j int) bool { return s.Brokers[i].ID < s.Brokers[j].ID })

	reassign.Each(func(r kadm.ListPartitionReassignmentsResponse) {
		s.Reassignments = append(s.Reassignments, Reassignment{Topic: r.Topic, Partition: r.Partition,
			Replicas: r.Replicas, Adding: r.AddingReplicas, Removing: r.RemovingReplicas})
	})
	sort.Slice(s.Reassignments, func(i, j int) bool {
		a, b := s.Reassignments[i], s.Reassignments[j]
		return a.Topic < b.Topic || (a.Topic == b.Topic && a.Partition < b.Partition)
	})
	s.Groups = groups
	s.FillEmpty()
	sort.Strings(s.Errors)
	s.DurationMs = time.Since(start).Milliseconds()
	return s, nil
}

// FillEmpty replaces nil lists with empty ones so the JSON never carries null arrays.
func (s *Snapshot) FillEmpty() {
	if s.Brokers == nil {
		s.Brokers = []Broker{}
	}
	if s.Features == nil {
		s.Features = []Feature{}
	}
	if s.Topics == nil {
		s.Topics = []Topic{}
	}
	if s.Reassignments == nil {
		s.Reassignments = []Reassignment{}
	}
	if s.Groups == nil {
		s.Groups = []Group{}
	}
}

// ShareGroupNames lists the share groups (KIP-932); clusters without share groups return none.
func (c *Client) ShareGroupNames(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	listed, err := c.Adm.ListGroupsByType(ctx, []string{GroupTypeShare})
	if err != nil {
		return out
	}
	for name := range listed {
		out[name] = true
	}
	return out
}

func (c *Client) groupSummaries(ctx context.Context, maxGroups int) ([]Group, error) {
	listed, err := c.Adm.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	share := c.ShareGroupNames(ctx)
	for name := range share {
		if _, ok := listed[name]; !ok {
			listed[name] = kadm.ListedGroup{Group: name, ProtocolType: GroupTypeShare}
		}
	}
	names := listed.Groups()
	sort.Strings(names)
	if maxGroups > 0 && len(names) > maxGroups {
		names = names[:maxGroups]
	}
	out := make([]Group, 0, len(names))
	if len(names) == 0 {
		return out, nil
	}
	var consumers, shares []string
	for _, name := range names {
		if share[name] {
			shares = append(shares, name)
		} else {
			consumers = append(consumers, name)
		}
	}
	lags := kadm.DescribedGroupLags{}
	if len(consumers) > 0 {
		if lags, err = c.Adm.Lag(ctx, consumers...); err != nil {
			return nil, err
		}
	}
	shareGroups := c.shareSummaries(ctx, shares)
	for _, name := range names {
		if g, ok := shareGroups[name]; ok {
			out = append(out, g)
			continue
		}
		g := Group{ID: name, Type: GroupTypeConsumer, ProtocolType: listed[name].ProtocolType, State: listed[name].State}
		if l, ok := lags[name]; ok {
			g.State = l.State
			g.Members = len(l.Members)
			if err := l.Error(); err != nil {
				g.Error = err.Error()
			}
			for topic, parts := range l.Lag {
				g.Topics = append(g.Topics, topic)
				for _, pl := range parts {
					if pl.Lag > 0 {
						g.Lag += pl.Lag
					}
				}
			}
			sort.Strings(g.Topics)
		}
		out = append(out, g)
	}
	return out, nil
}

// shareSummaries describes share groups: state, members, the topics they consume and the lag
// of their share-partitions.
func (c *Client) shareSummaries(ctx context.Context, names []string) map[string]Group {
	out := map[string]Group{}
	if len(names) == 0 {
		return out
	}
	described, err := c.Adm.DescribeShareGroups(ctx, names...)
	if err != nil && len(described) == 0 {
		for _, name := range names {
			out[name] = Group{ID: name, Type: GroupTypeShare, ProtocolType: GroupTypeShare, Error: err.Error()}
		}
		return out
	}
	offsets, _ := c.Adm.DescribeShareGroupOffsets(ctx, names...)
	for _, name := range names {
		g := Group{ID: name, Type: GroupTypeShare, ProtocolType: GroupTypeShare}
		d, ok := described[name]
		if !ok {
			g.Error = "not described"
			out[name] = g
			continue
		}
		g.State, g.Members = d.GroupState, len(d.Members)
		if d.Err != nil {
			g.Error = d.Err.Error()
		}
		topics := map[string]bool{}
		for _, m := range d.Members {
			for _, t := range m.SubscribedTopicNames {
				topics[t] = true
			}
		}
		for t, parts := range offsets[name].Offsets {
			topics[t] = true
			for _, o := range parts {
				if o.Lag > 0 {
					g.Lag += o.Lag
				}
			}
		}
		for t := range topics {
			g.Topics = append(g.Topics, t)
		}
		sort.Strings(g.Topics)
		out[name] = g
	}
	return out
}
