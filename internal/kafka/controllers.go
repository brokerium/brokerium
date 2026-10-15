// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package kafka

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ControllerClient talks to KRaft controllers directly, the way bootstrap.controllers does in
// the Java admin client (KIP-919). franz-go has no such mode and controllers do not answer
// Metadata, so every request goes to a seed connection instead of being routed by metadata.
// Requests that only the active controller serves are tried on each controller in turn.
type ControllerClient struct {
	kgo *kgo.Client
}

// NewControllerClient connects to the controller listeners in p.BootstrapServers.
func NewControllerClient(p Profile) (*ControllerClient, error) {
	c, err := NewClient(p)
	if err != nil {
		return nil, err
	}
	return &ControllerClient{kgo: c.KGO}, nil
}

// Close releases connections.
func (c *ControllerClient) Close() { c.kgo.Close() }

// ControllerInfo is a controller as reported by DescribeCluster with the controller endpoint type.
type ControllerInfo struct {
	NodeID int32  `json:"nodeId"`
	Host   string `json:"host"`
	Port   int32  `json:"port"`
	Rack   string `json:"rack"`
}

// DescribeControllers returns the cluster id, the active controller and all controllers.
func (c *ControllerClient) DescribeControllers(ctx context.Context) (string, int32, []ControllerInfo, error) {
	req := kmsg.NewPtrDescribeClusterRequest()
	req.EndpointType = 2
	var resp *kmsg.DescribeClusterResponse
	err := c.each(ctx, req, func(r kmsg.Response) error {
		resp = r.(*kmsg.DescribeClusterResponse)
		return errorFor(resp.ErrorCode, resp.ErrorMessage)
	})
	if err != nil {
		return "", 0, nil, fmt.Errorf("describe controllers: %w", err)
	}
	out := make([]ControllerInfo, 0, len(resp.Brokers))
	for _, b := range resp.Brokers {
		ci := ControllerInfo{NodeID: b.NodeID, Host: b.Host, Port: b.Port}
		if b.Rack != nil {
			ci.Rack = *b.Rack
		}
		out = append(out, ci)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return resp.ClusterID, resp.ControllerID, out, nil
}

// DescribeQuorum asks the controllers for the quorum state; only the leader answers it.
func (c *ControllerClient) DescribeQuorum(ctx context.Context) (*Quorum, error) {
	var q *Quorum
	err := c.each(ctx, describeQuorumRequest(), func(r kmsg.Response) error {
		var err error
		q, err = parseQuorum(r.(*kmsg.DescribeQuorumResponse))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("describe quorum: %w", err)
	}
	return q, nil
}

// each sends req to every seed until check accepts a response. Responses that point to another
// node (not the leader, not the controller) move on to the next seed.
func (c *ControllerClient) each(ctx context.Context, req kmsg.Request, check func(kmsg.Response) error) error {
	seeds := c.kgo.SeedBrokers()
	if len(seeds) == 0 {
		return errors.New("no controller addresses")
	}
	var errs []error
	for _, b := range seeds {
		resp, err := b.Request(ctx, req)
		if err == nil {
			err = check(resp)
		}
		if err == nil {
			return nil
		}
		errs = append(errs, err)
		if ctx.Err() != nil || !tryNextController(err) {
			break
		}
	}
	return errors.Join(errs...)
}

// tryNextController reports whether another controller may answer: the connection failed or
// the node is not the quorum leader.
func tryNextController(err error) bool {
	var ke *kerr.Error
	if !errors.As(err, &ke) {
		return true
	}
	return ke == kerr.NotLeaderForPartition || ke == kerr.NotController
}
