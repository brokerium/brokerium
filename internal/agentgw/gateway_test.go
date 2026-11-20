// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentgw

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/brokerium/brokerium/internal/store"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// idleStream is the stream of an agent that sends nothing until its context ends.
type idleStream struct {
	ctx context.Context
}

func (s idleStream) Context() context.Context             { return s.ctx }
func (s idleStream) Send(*agentv1.ServerMessage) error    { return nil }
func (s idleStream) Recv() (*agentv1.AgentMessage, error) { <-s.ctx.Done(); return nil, s.ctx.Err() }

// connect serves a session of node id and returns the error its stream ends with.
func connect(t *testing.T, g *Gateway, id string) <-chan error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ended := make(chan error, 1)
	go func() { ended <- g.serve(&store.Node{ID: id, Name: id}, idleStream{ctx}, nil) }()
	return ended
}

func waitConnected(t *testing.T, g *Gateway, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !g.Connected(id) {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not connect", id)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func ended(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the session is still open")
		return nil
	}
}

func TestNodeDeletedEndsSession(t *testing.T) {
	t.Parallel()
	g := New(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	deleted, kept := connect(t, g, "n1"), connect(t, g, "n2")
	waitConnected(t, g, "n1")
	waitConnected(t, g, "n2")

	g.NodeDeleted("n1")
	if err := ended(t, deleted); status.Code(err) != codes.NotFound {
		t.Errorf("the session of the deleted node ended with %v, want NotFound", err)
	}
	if g.Connected("n1") {
		t.Error("the deleted node is still connected")
	}
	stop := &agentv1.Command{Action: &agentv1.Command_Stop{Stop: &agentv1.StopKafka{}}}
	if _, err := g.Send(context.Background(), "n1", stop); !errors.Is(err, ErrNotConnected) {
		t.Errorf("a command to the deleted node: %v, want ErrNotConnected", err)
	}
	// An agent that authenticated before the deletion gets no session either.
	if err := ended(t, connect(t, g, "n1")); status.Code(err) != codes.NotFound {
		t.Errorf("a later session of the deleted node ended with %v, want NotFound", err)
	}

	if !g.Connected("n2") {
		t.Fatal("deleting n1 ended the session of n2")
	}
	newer := connect(t, g, "n2")
	if err := ended(t, kept); err == nil || status.Code(err) == codes.NotFound {
		t.Errorf("a replaced session ended with %v; its agent must connect again, not stop", err)
	}
	waitConnected(t, g, "n2")
	select {
	case err := <-newer:
		t.Fatalf("the newer session of n2 ended: %v", err)
	default:
	}
}
