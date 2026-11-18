// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

// testStore connects to BROKERIUM_TEST_DATABASE_URL and applies the migrations. Tests that need
// PostgreSQL are skipped without it; they create their own rows and remove them again.
func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("BROKERIUM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set BROKERIUM_TEST_DATABASE_URL to a PostgreSQL database to run the store tests")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNodeAssignment(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run := NewID()
	cluster := func(suffix string) *Cluster {
		c := &Cluster{Name: "test-" + run + suffix, Provider: ProviderAgent}
		if err := s.CreateCluster(ctx, c); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.DeleteCluster(context.Background(), c.ID) })
		return c
	}
	node := func(name string) *Node {
		n := &Node{Name: run + "-" + name, Status: json.RawMessage("{}")}
		if err := s.CreateNode(ctx, n); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.DeleteNode(context.Background(), n.ID) })
		return n
	}
	get := func(id string) *Node {
		n, err := s.GetNode(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	c1, c2 := cluster("-1"), cluster("-2")
	a, b := node("a"), node("b")

	if err := s.UpdateNodeEnrollment(ctx, a.ID, "fp1", time.Now().Add(time.Hour), "host-a", "0.3.1"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateNodeInsecureEnrollment(ctx, a.ID, []byte("sealed"), "host-a2", "0.3.1"); err != nil {
		t.Fatal(err)
	}
	if n := get(a.ID); n.Hostname != "host-a2" || n.AdvertisedHost != "host-a" {
		t.Errorf("after enrolling again: hostname %q, advertised host %q; want host-a2 and the first advertised host", n.Hostname, n.AdvertisedHost)
	}

	steps := []struct {
		name string
		do   func() error
		want error
	}{
		{"claim a free node", func() error { return s.ClaimNode(ctx, a.ID, c1.ID, "broker", 101) }, nil},
		{"claim the same assignment again", func() error { return s.ClaimNode(ctx, a.ID, c1.ID, "broker", 101) }, nil},
		{"claim another node id", func() error { return s.ClaimNode(ctx, a.ID, c1.ID, "broker", 102) }, ErrConflict},
		{"claim another role", func() error { return s.ClaimNode(ctx, a.ID, c1.ID, "controller", 101) }, ErrConflict},
		{"claim for another cluster", func() error { return s.ClaimNode(ctx, a.ID, c2.ID, "broker", 101) }, ErrConflict},
		{"claim an id another node has", func() error { return s.ClaimNode(ctx, b.ID, c1.ID, "broker", 101) }, ErrConflict},
		{"claim an unknown node", func() error { return s.ClaimNode(ctx, "missing", c1.ID, "broker", 103) }, ErrNotFound},
		{"claim an invalid role", func() error { return s.ClaimNode(ctx, b.ID, c1.ID, "voter", 1) }, errAny},
		{"claim a combined node", func() error { return s.ClaimNode(ctx, b.ID, c1.ID, "combined", 1) }, nil},
		{"release with another node id", func() error { return s.ReleaseNode(ctx, a.ID, c1.ID, 102) }, nil},
	}
	for _, st := range steps {
		err := st.do()
		if (st.want == errAny && err == nil) || (st.want != errAny && !errors.Is(err, st.want)) {
			t.Errorf("%s: %v, want %v", st.name, err, st.want)
		}
	}
	if n := get(a.ID); n.Role != "broker" || n.ClusterID == nil || *n.ClusterID != c1.ID {
		t.Errorf("a release with another node id changed the node: %+v", n)
	}
	if err := s.ReleaseNode(ctx, a.ID, c1.ID, 101); err != nil {
		t.Fatal(err)
	}
	if n := get(a.ID); n.Role != "" || n.ClusterID != nil || n.KafkaNodeID != nil {
		t.Errorf("released node %+v", n)
	}

	a.Name, a.AdvertisedHost, a.Rack = b.Name, "kafka-a.example.com", "rack-a"
	if err := s.UpdateNodeDetails(ctx, a); !errors.Is(err, ErrConflict) {
		t.Errorf("renaming to a taken name: %v", err)
	}
	a.Name = run + "-renamed"
	if err := s.UpdateNodeDetails(ctx, a); err != nil {
		t.Fatal(err)
	}
	if n := get(a.ID); n.Name != a.Name || n.AdvertisedHost != "kafka-a.example.com" || n.Rack != "rack-a" {
		t.Errorf("updated node %+v", n)
	}

	tok := &JoinToken{TokenHash: "test-" + run, NodeID: &a.ID, CreatedBy: "test", ExpiresAt: time.Now().Add(time.Hour)}
	if err := s.CreateJoinToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	used, err := s.ConsumeJoinToken(ctx, tok.TokenHash)
	if err != nil || used.NodeID == nil || *used.NodeID != a.ID || used.NodeName != "" {
		t.Errorf("consumed token %+v, %v", used, err)
	}

	if err := s.DeleteCluster(ctx, c1.ID); err != nil {
		t.Fatal(err)
	}
	if n := get(b.ID); n.Role != "" || n.ClusterID != nil || n.KafkaNodeID != nil {
		t.Errorf("a node of a deleted cluster kept its assignment: %+v", n)
	}
}

// errAny stands for any error in the table of TestNodeAssignment.
var errAny = errors.New("any error")
