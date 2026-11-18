// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentgw

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/brokerium/brokerium/internal/store"
)

func TestNodeNames(t *testing.T) {
	t.Parallel()
	for name, ok := range map[string]bool{"kafka-17": true, "Kafka_17.example.com": true, "a": true, strings.Repeat("a", 63): true,
		"": false, strings.Repeat("a", 64): false, "kafka 17": false, "kafka/17": false, "kafka:17": false, "kafkä": false} {
		if ValidNodeName(name) != ok {
			t.Errorf("ValidNodeName(%q) = %v, want %v", name, !ok, ok)
		}
	}
	for host, want := range map[string]string{
		"kafka-17":               "kafka-17",
		"kafka-17.example.com":   "kafka-17.example.com",
		"Kafka 17":               "Kafka-17",
		"kafkä:1":                "kafk--1",
		"":                       "node",
		strings.Repeat("h", 70):  strings.Repeat("h", 63),
		"host/with\nnewline/and": "host-with-newline-and",
	} {
		got := NodeNameFromHost(host)
		if got != want || !ValidNodeName(got) {
			t.Errorf("NodeNameFromHost(%q) = %q, want %q", host, got, want)
		}
	}
	long := strings.Repeat("h", 63)
	for _, tt := range []struct {
		base string
		n    int
		want string
	}{
		{"kafka", 1, "kafka"},
		{"kafka", 2, "kafka-2"},
		{"kafka", 12, "kafka-12"},
		{long, 2, strings.Repeat("h", 61) + "-2"},
		{long, 100, strings.Repeat("h", 59) + "-100"},
	} {
		if got := numberedName(tt.base, tt.n); got != tt.want || !ValidNodeName(got) {
			t.Errorf("numberedName(%q, %d) = %q, want %q", tt.base, tt.n, got, tt.want)
		}
	}
}

// memNodes keeps nodes like the store: ids and names are unique.
type memNodes map[string]*store.Node

func (m memNodes) GetNode(_ context.Context, id string) (*store.Node, error) {
	if n, ok := m[id]; ok {
		return n, nil
	}
	return nil, store.ErrNotFound
}

func (m memNodes) GetNodeByName(_ context.Context, name string) (*store.Node, error) {
	for _, n := range m {
		if n.Name == name {
			return n, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m memNodes) CreateNode(ctx context.Context, n *store.Node) error {
	if _, err := m.GetNodeByName(ctx, n.Name); err == nil {
		return store.ErrConflict
	}
	n.ID = "id-" + n.Name
	m[n.ID] = n
	return nil
}

func TestEnrolledNode(t *testing.T) {
	t.Parallel()
	cluster, existing := "c1", "n1"
	tests := []struct {
		name     string
		tok      store.JoinToken
		hostname string
		want     string
		cluster  *string
		code     codes.Code
	}{
		{"named after the host", store.JoinToken{}, "kafka-9", "kafka-9", nil, codes.OK},
		{"the host name is taken", store.JoinToken{ClusterID: &cluster}, "kafka-1", "kafka-1-2", &cluster, codes.OK},
		{"the host name and its first numbers are taken", store.JoinToken{}, "kafka-3", "kafka-3-4", nil, codes.OK},
		{"a host name that is no node name", store.JoinToken{}, "kafka 9.example.com", "kafka-9.example.com", nil, codes.OK},
		{"no host name", store.JoinToken{}, "", "node", nil, codes.OK},
		{"a named token creates the node", store.JoinToken{NodeName: "broker-a", ClusterID: &cluster}, "kafka-1", "broker-a", &cluster, codes.OK},
		{"a named token enrolls the node of that name again", store.JoinToken{NodeName: "kafka-1"}, "other", "kafka-1", nil, codes.OK},
		{"a node id enrolls that node again", store.JoinToken{NodeID: &existing}, "reinstalled", "kafka-1", nil, codes.OK},
		{"the node of the node id is gone", store.JoinToken{NodeID: func() *string { s := "gone"; return &s }()}, "kafka-1", "", nil,
			codes.PermissionDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			nodes := memNodes{
				"n1": {ID: "n1", Name: "kafka-1"},
				"n3": {ID: "n3", Name: "kafka-3"}, "n32": {ID: "n32", Name: "kafka-3-2"}, "n33": {ID: "n33", Name: "kafka-3-3"},
			}
			before := len(nodes)
			n, err := enrolledNode(context.Background(), nodes, &tt.tok, tt.hostname)
			if status.Code(err) != tt.code {
				t.Fatalf("error %v, want code %s", err, tt.code)
			}
			if err != nil {
				return
			}
			if n.Name != tt.want {
				t.Errorf("node %q, want %q", n.Name, tt.want)
			}
			if (tt.cluster == nil) != (n.ClusterID == nil) {
				t.Errorf("cluster %v, want %v", n.ClusterID, tt.cluster)
			}
			created := len(nodes) - before
			if reused := tt.tok.NodeID != nil || tt.want == "kafka-1"; reused == (created == 1) {
				t.Errorf("%d node(s) created", created)
			}
		})
	}
}
