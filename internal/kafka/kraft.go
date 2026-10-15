// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package kafka

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// MetadataTopic is the KRaft metadata log.
const MetadataTopic = "__cluster_metadata"

// Endpoint is a listener of a node.
type Endpoint struct {
	Name string `json:"name"`
	Host string `json:"host"`
	Port int32  `json:"port"`
}

// QuorumReplica is a voter or observer of the metadata log.
type QuorumReplica struct {
	NodeID                int32      `json:"nodeId"`
	DirectoryID           string     `json:"directoryId"`
	Role                  string     `json:"role"`
	LogEndOffset          int64      `json:"logEndOffset"`
	Lag                   int64      `json:"lag"`
	LastFetchTimestamp    int64      `json:"lastFetchTimestamp"`
	LastCaughtUpTimestamp int64      `json:"lastCaughtUpTimestamp"`
	Endpoints             []Endpoint `json:"endpoints,omitempty"`
}

// Quorum describes the KRaft controller quorum.
type Quorum struct {
	LeaderID      int32           `json:"leaderId"`
	LeaderEpoch   int32           `json:"leaderEpoch"`
	HighWatermark int64           `json:"highWatermark"`
	Voters        []QuorumReplica `json:"voters"`
	Observers     []QuorumReplica `json:"observers"`
}

// Voter returns the voter with the given id.
func (q *Quorum) Voter(id int32) (QuorumReplica, bool) {
	for _, v := range q.Voters {
		if v.NodeID == id {
			return v, true
		}
	}
	return QuorumReplica{}, false
}

// Observer returns the observer with the given id.
func (q *Quorum) Observer(id int32) (QuorumReplica, bool) {
	for _, v := range q.Observers {
		if v.NodeID == id {
			return v, true
		}
	}
	return QuorumReplica{}, false
}

// EncodeUUID renders a Kafka UUID the way Kafka prints it (URL-safe base64 without padding).
func EncodeUUID(id [16]byte) string {
	if id == ([16]byte{}) {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(id[:])
}

// DecodeUUID parses a Kafka UUID string.
func DecodeUUID(s string) ([16]byte, error) {
	var out [16]byte
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) != 16 {
		return out, fmt.Errorf("invalid Kafka UUID %q", s)
	}
	copy(out[:], raw)
	return out, nil
}

func errorFor(code int16, msg *string) error {
	if code == 0 {
		return nil
	}
	err := kerr.ErrorForCode(code)
	if msg != nil && *msg != "" {
		return fmt.Errorf("%w: %s", err, *msg)
	}
	return err
}

// DescribeQuorum returns the voters, observers and leader of the metadata quorum.
func (c *Client) DescribeQuorum(ctx context.Context) (*Quorum, error) {
	resp, err := describeQuorumRequest().RequestWith(ctx, c.KGO)
	if err != nil {
		return nil, fmt.Errorf("describe quorum: %w", err)
	}
	return parseQuorum(resp)
}

func describeQuorumRequest() *kmsg.DescribeQuorumRequest {
	req := kmsg.NewPtrDescribeQuorumRequest()
	t := kmsg.NewDescribeQuorumRequestTopic()
	t.Topic = MetadataTopic
	p := kmsg.NewDescribeQuorumRequestTopicPartition()
	p.Partition = 0
	t.Partitions = append(t.Partitions, p)
	req.Topics = append(req.Topics, t)
	return req
}

func parseQuorum(resp *kmsg.DescribeQuorumResponse) (*Quorum, error) {
	if err := errorFor(resp.ErrorCode, resp.ErrorMessage); err != nil {
		return nil, fmt.Errorf("describe quorum: %w", err)
	}
	endpoints := map[int32][]Endpoint{}
	for _, n := range resp.Nodes {
		for _, l := range n.Listeners {
			endpoints[n.NodeID] = append(endpoints[n.NodeID], Endpoint{Name: l.Name, Host: l.Host, Port: int32(l.Port)})
		}
	}
	if len(resp.Topics) == 0 || len(resp.Topics[0].Partitions) == 0 {
		return nil, fmt.Errorf("describe quorum: empty response")
	}
	part := resp.Topics[0].Partitions[0]
	if err := errorFor(part.ErrorCode, part.ErrorMessage); err != nil {
		return nil, fmt.Errorf("describe quorum: %w", err)
	}
	q := &Quorum{LeaderID: part.LeaderID, LeaderEpoch: part.LeaderEpoch, HighWatermark: part.HighWatermark}
	var leaderLEO int64
	for _, v := range part.CurrentVoters {
		if v.ReplicaID == part.LeaderID {
			leaderLEO = v.LogEndOffset
		}
	}
	convert := func(rs []kmsg.DescribeQuorumResponseTopicPartitionReplicaState, voter bool) []QuorumReplica {
		out := make([]QuorumReplica, 0, len(rs))
		for _, r := range rs {
			role := "observer"
			if voter {
				role = "follower"
				if r.ReplicaID == part.LeaderID {
					role = "leader"
				}
			}
			lag := leaderLEO - r.LogEndOffset
			if lag < 0 || r.LogEndOffset < 0 {
				lag = 0
			}
			out = append(out, QuorumReplica{
				NodeID: r.ReplicaID, DirectoryID: EncodeUUID(r.ReplicaDirectoryID), Role: role,
				LogEndOffset: r.LogEndOffset, Lag: lag, LastFetchTimestamp: r.LastFetchTimestamp,
				LastCaughtUpTimestamp: r.LastCaughtUpTimestamp, Endpoints: endpoints[r.ReplicaID],
			})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
		return out
	}
	q.Voters = convert(part.CurrentVoters, true)
	q.Observers = convert(part.Observers, false)
	return q, nil
}

// BrokerInfo is a registered broker as reported by DescribeCluster.
type BrokerInfo struct {
	NodeID int32  `json:"nodeId"`
	Host   string `json:"host"`
	Port   int32  `json:"port"`
	Rack   string `json:"rack"`
	Fenced bool   `json:"fenced"`
}

// ClusterInfo is the result of DescribeCluster.
type ClusterInfo struct {
	ClusterID    string       `json:"clusterId"`
	ControllerID int32        `json:"controllerId"`
	Brokers      []BrokerInfo `json:"brokers"`
}

// DescribeCluster returns registered brokers, including fenced ones when supported (KIP-1073).
func (c *Client) DescribeCluster(ctx context.Context, includeFenced bool) (*ClusterInfo, error) {
	req := kmsg.NewPtrDescribeClusterRequest()
	req.IncludeFencedBrokers = includeFenced
	req.EndpointType = 1
	resp, err := req.RequestWith(ctx, c.KGO)
	if err != nil {
		return nil, fmt.Errorf("describe cluster: %w", err)
	}
	if err := errorFor(resp.ErrorCode, resp.ErrorMessage); err != nil {
		return nil, fmt.Errorf("describe cluster: %w", err)
	}
	info := &ClusterInfo{ClusterID: resp.ClusterID, ControllerID: resp.ControllerID}
	for _, b := range resp.Brokers {
		bi := BrokerInfo{NodeID: b.NodeID, Host: b.Host, Port: b.Port, Fenced: b.IsFenced}
		if b.Rack != nil {
			bi.Rack = *b.Rack
		}
		info.Brokers = append(info.Brokers, bi)
	}
	sort.Slice(info.Brokers, func(i, j int) bool { return info.Brokers[i].NodeID < info.Brokers[j].NodeID })
	return info, nil
}

// AddRaftVoter promotes an observer controller to a voter (KIP-853).
func (c *Client) AddRaftVoter(ctx context.Context, clusterID string, voterID int32, directoryID string, endpoints []Endpoint) error {
	dir, err := DecodeUUID(directoryID)
	if err != nil {
		return err
	}
	req := kmsg.NewPtrAddRaftVoterRequest()
	if clusterID != "" {
		req.ClusterID = &clusterID
	}
	req.TimeoutMillis = 30000
	req.VoterID = voterID
	req.VoterDirectoryID = dir
	req.AckWhenCommitted = true
	for _, e := range endpoints {
		l := kmsg.NewAddRaftVoterRequestListener()
		l.Name, l.Host, l.Port = e.Name, e.Host, uint16(e.Port)
		req.Listeners = append(req.Listeners, l)
	}
	resp, err := req.RequestWith(ctx, c.KGO)
	if err != nil {
		return fmt.Errorf("add raft voter %d: %w", voterID, err)
	}
	return errorFor(resp.ErrorCode, resp.ErrorMessage)
}

// RemoveRaftVoter removes a voter from the quorum (KIP-853).
func (c *Client) RemoveRaftVoter(ctx context.Context, clusterID string, voterID int32, directoryID string) error {
	dir, err := DecodeUUID(directoryID)
	if err != nil {
		return err
	}
	req := kmsg.NewPtrRemoveRaftVoterRequest()
	if clusterID != "" {
		req.ClusterID = &clusterID
	}
	req.VoterID = voterID
	req.VoterDirectoryID = dir
	resp, err := req.RequestWith(ctx, c.KGO)
	if err != nil {
		return fmt.Errorf("remove raft voter %d: %w", voterID, err)
	}
	return errorFor(resp.ErrorCode, resp.ErrorMessage)
}

// UnregisterBroker removes a broker registration (KIP-631).
func (c *Client) UnregisterBroker(ctx context.Context, brokerID int32) error {
	req := kmsg.NewPtrUnregisterBrokerRequest()
	req.BrokerID = brokerID
	resp, err := req.RequestWith(ctx, c.KGO)
	if err != nil {
		return fmt.Errorf("unregister broker %d: %w", brokerID, err)
	}
	return errorFor(resp.ErrorCode, resp.ErrorMessage)
}

// Feature is a finalized or supported feature level.
type Feature struct {
	Name         string `json:"name"`
	Finalized    int16  `json:"finalized"`
	SupportedMin int16  `json:"supportedMin"`
	SupportedMax int16  `json:"supportedMax"`
}

// Features returns finalized and supported feature levels as seen by a broker.
func (c *Client) Features(ctx context.Context) ([]Feature, error) {
	req := kmsg.NewPtrApiVersionsRequest()
	req.ClientSoftwareName = "brokerium"
	req.ClientSoftwareVersion = "1"
	resp, err := req.RequestWith(ctx, c.KGO)
	if err != nil {
		return nil, fmt.Errorf("api versions: %w", err)
	}
	if err := errorFor(resp.ErrorCode, nil); err != nil {
		return nil, err
	}
	byName := map[string]*Feature{}
	for _, f := range resp.SupportedFeatures {
		byName[f.Name] = &Feature{Name: f.Name, Finalized: -1, SupportedMin: f.MinVersion, SupportedMax: f.MaxVersion}
	}
	for _, f := range resp.FinalizedFeatures {
		ft, ok := byName[f.Name]
		if !ok {
			ft = &Feature{Name: f.Name}
			byName[f.Name] = ft
		}
		ft.Finalized = f.MaxVersionLevel
	}
	out := make([]Feature, 0, len(byName))
	for _, f := range byName {
		if f.Finalized < 0 {
			f.Finalized = 0
		}
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// UpdateFeature upgrades (or validates the upgrade of) one feature to a level.
func (c *Client) UpdateFeature(ctx context.Context, name string, level int16, validateOnly bool) error {
	resp, err := c.Adm.UpdateFeatures(ctx, validateOnly, kadm.FeatureUpdate{Feature: name, MaxVersionLevel: level, UpgradeType: 1})
	if err != nil {
		return fmt.Errorf("update feature %s: %w", name, err)
	}
	for _, r := range resp.Sorted() {
		if r.Err != nil {
			return fmt.Errorf("update feature %s: %w", r.Feature, r.Err)
		}
	}
	return nil
}

// WaitFor polls fn until it returns true, an error, or the context ends.
func WaitFor(ctx context.Context, every time.Duration, fn func(context.Context) (bool, error)) error {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		ok, err := fn(ctx)
		if err != nil || ok {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}
