// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentgw

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/brokerium/brokerium/internal/auth"
	"github.com/brokerium/brokerium/internal/store"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// TokenPrefix starts every join token: brk1.<secret>.<CA fingerprint>.
const TokenPrefix = "brk1"

// ErrNotConnected is returned when a command targets an offline agent.
var ErrNotConnected = errors.New("agent is not connected")

type session struct {
	nodeID  string
	send    chan *agentv1.ServerMessage
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	waiters map[string]chan *agentv1.CommandResult
}

func (s *session) close() { s.once.Do(func() { close(s.done) }) }

// Gateway serves the agent protocol and dispatches commands to connected agents.
type Gateway struct {
	agentv1.UnimplementedAgentServiceServer
	Store *store.Store
	CA    *CA
	Log   *slog.Logger

	mu       sync.Mutex
	sessions map[string]*session
	seq      uint64
}

// New creates a gateway.
func New(st *store.Store, ca *CA, log *slog.Logger) *Gateway {
	return &Gateway{Store: st, CA: ca, Log: log, sessions: map[string]*session{}}
}

// NewJoinToken creates a single-use token for a node name.
func (g *Gateway) NewJoinToken(ctx context.Context, nodeName string, clusterID *string, ttl time.Duration, by string) (string, time.Time, error) {
	secret, hash := auth.NewToken()
	expires := time.Now().Add(ttl)
	if err := g.Store.CreateJoinToken(ctx, &store.JoinToken{TokenHash: hash, NodeName: nodeName, ClusterID: clusterID,
		CreatedBy: by, ExpiresAt: expires}); err != nil {
		return "", time.Time{}, err
	}
	return strings.Join([]string{TokenPrefix, secret, g.CA.Fingerprint()}, "."), expires, nil
}

// Serve starts the gRPC server on addr until ctx ends.
func (g *Gateway) Serve(ctx context.Context, addr string, serverNames []string) error {
	cert, err := g.CA.ServerCertificate(serverNames)
	if err != nil {
		return err
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    g.CA.Pool(),
	}
	srv := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsCfg)),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: 30 * time.Second, Timeout: 10 * time.Second}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 10 * time.Second, PermitWithoutStream: true}),
	)
	agentv1.RegisterAgentServiceServer(srv, g)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()
	g.Log.Info("agent gateway listening", "addr", addr, "caFingerprint", g.CA.Fingerprint()[:16])
	return srv.Serve(lis)
}

// Enroll exchanges a join token and CSR for a client certificate.
func (g *Gateway) Enroll(ctx context.Context, req *agentv1.EnrollRequest) (*agentv1.EnrollResponse, error) {
	parts := strings.Split(req.GetToken(), ".")
	if len(parts) != 3 || parts[0] != TokenPrefix {
		return nil, status.Error(codes.InvalidArgument, "malformed join token")
	}
	tok, err := g.Store.ConsumeJoinToken(ctx, auth.HashToken(parts[1]))
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "join token is invalid, used or expired")
	}
	node, err := g.Store.GetNodeByName(ctx, tok.NodeName)
	if errors.Is(err, store.ErrNotFound) {
		node = &store.Node{Name: tok.NodeName, ClusterID: tok.ClusterID, Status: json.RawMessage("{}")}
		if err := g.Store.CreateNode(ctx, node); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	der, fp, err := g.CA.SignAgent(req.GetCsrDer(), node.ID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := g.Store.UpdateNodeEnrollment(ctx, node.ID, fp, req.GetHostname(), req.GetAgentVersion()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	g.Log.Info("agent enrolled", "node", node.Name, "id", node.ID, "hostname", req.GetHostname())
	return &agentv1.EnrollResponse{NodeId: node.ID, NodeName: node.Name, CertDer: der, CaCertDer: g.CA.DER}, nil
}

func (g *Gateway) authenticate(ctx context.Context) (*store.Node, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no peer")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.PeerCertificates) == 0 {
		return nil, status.Error(codes.Unauthenticated, "client certificate required")
	}
	leaf := info.State.PeerCertificates[0]
	node, err := g.Store.GetNode(ctx, leaf.Subject.CommonName)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "unknown node")
	}
	if fingerprint(leaf) != node.CertFingerprint {
		return nil, status.Error(codes.PermissionDenied, "certificate was replaced by a newer enrollment")
	}
	return node, nil
}

func fingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return hex.EncodeToString(sum[:])
}

// Connect is the long-lived command stream of an agent.
func (g *Gateway) Connect(stream agentv1.AgentService_ConnectServer) error {
	node, err := g.authenticate(stream.Context())
	if err != nil {
		return err
	}
	s := &session{nodeID: node.ID, send: make(chan *agentv1.ServerMessage, 16), done: make(chan struct{}),
		waiters: map[string]chan *agentv1.CommandResult{}}
	g.mu.Lock()
	if old, ok := g.sessions[node.ID]; ok {
		old.close()
	}
	g.sessions[node.ID] = s
	g.mu.Unlock()
	g.Log.Info("agent connected", "node", node.Name)
	defer func() {
		s.close()
		g.mu.Lock()
		if g.sessions[node.ID] == s {
			delete(g.sessions, node.ID)
		}
		g.mu.Unlock()
		g.Log.Info("agent disconnected", "node", node.Name)
	}()

	errCh := make(chan error, 2)
	go func() {
		for {
			select {
			case msg := <-s.send:
				if err := stream.Send(msg); err != nil {
					errCh <- err
					return
				}
			case <-s.done:
				errCh <- errors.New("session replaced by a newer connection")
				return
			case <-stream.Context().Done():
				return
			}
		}
	}()
	go func() {
		for {
			in, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}
			g.handle(stream.Context(), s, in)
		}
	}()
	return <-errCh
}

func (g *Gateway) handle(ctx context.Context, s *session, in *agentv1.AgentMessage) {
	switch body := in.Body.(type) {
	case *agentv1.AgentMessage_Hello:
		g.saveStatus(ctx, s.nodeID, body.Hello.GetStatus())
	case *agentv1.AgentMessage_Status:
		g.saveStatus(ctx, s.nodeID, body.Status)
	case *agentv1.AgentMessage_Result:
		if body.Result.GetStatus() != nil {
			g.saveStatus(ctx, s.nodeID, body.Result.GetStatus())
		}
		s.mu.Lock()
		w, ok := s.waiters[body.Result.GetCommandId()]
		delete(s.waiters, body.Result.GetCommandId())
		s.mu.Unlock()
		if ok {
			w <- body.Result
		}
	}
}

// StatusJSON renders a node status with plain JSON numbers (protojson encodes int64 as strings).
func StatusJSON(st *agentv1.NodeStatus) json.RawMessage {
	m := map[string]any{
		"running": st.GetRunning(), "activeVersion": st.GetActiveVersion(), "installedVersions": st.GetInstalledVersions(),
		"dataBytes": st.GetDataBytes(), "diskFreeBytes": st.GetDiskFreeBytes(), "hostname": st.GetHostname(),
		"pid": st.GetPid(), "lastExit": st.GetLastExit(), "processRoles": st.GetProcessRoles(),
		"javaVersion": st.GetJavaVersion(), "adopted": st.GetAdopted(),
	}
	if meta := st.GetMeta(); meta != nil {
		m["meta"] = map[string]any{"clusterId": meta.GetClusterId(), "nodeId": meta.GetNodeId(),
			"directoryId": meta.GetDirectoryId(), "formatted": meta.GetFormatted()}
	}
	raw, _ := json.Marshal(m)
	return raw
}

func (g *Gateway) saveStatus(ctx context.Context, nodeID string, st *agentv1.NodeStatus) {
	if st == nil {
		return
	}
	raw := StatusJSON(st)
	ctx = context.WithoutCancel(ctx)
	if err := g.Store.UpdateNodeStatus(ctx, nodeID, raw); err != nil {
		g.Log.Warn("saving node status", "node", nodeID, "error", err)
	}
	g.adopt(ctx, nodeID, st)
}

// adopt binds an adopted node, joined with a cluster's token, to its Kafka node id and role
// as soon as it reports its meta.properties.
func (g *Gateway) adopt(ctx context.Context, nodeID string, st *agentv1.NodeStatus) {
	meta := st.GetMeta()
	if !st.GetAdopted() || meta == nil || !meta.GetFormatted() {
		return
	}
	role, ok := AdoptedRole(st.GetProcessRoles())
	if !ok {
		return
	}
	n, err := g.Store.GetNode(ctx, nodeID)
	if err != nil || n.ClusterID == nil || n.KafkaNodeID != nil {
		return
	}
	id := meta.GetNodeId()
	if err := g.Store.AssignNode(ctx, nodeID, n.ClusterID, role, &id); err != nil {
		g.Log.Warn("adopting node", "node", n.Name, "error", err)
		return
	}
	g.Log.Info("adopted node", "node", n.Name, "kafkaNodeId", id, "role", role)
}

// AdoptedRole maps process.roles of an adopted node to a Brokerium role; combined
// broker+controller nodes are not managed.
func AdoptedRole(processRoles string) (string, bool) {
	switch strings.ReplaceAll(processRoles, " ", "") {
	case "broker":
		return "broker", true
	case "controller":
		return "controller", true
	}
	return "", false
}

// Connected reports whether a node has an open stream.
func (g *Gateway) Connected(nodeID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.sessions[nodeID]
	return ok
}

// Send delivers a command and waits for its result.
func (g *Gateway) Send(ctx context.Context, nodeID string, cmd *agentv1.Command) (*agentv1.CommandResult, error) {
	g.mu.Lock()
	s, ok := g.sessions[nodeID]
	g.seq++
	cmd.Id = fmt.Sprintf("c%d-%d", time.Now().UnixMilli(), g.seq)
	g.mu.Unlock()
	if !ok {
		return nil, ErrNotConnected
	}
	w := make(chan *agentv1.CommandResult, 1)
	s.mu.Lock()
	s.waiters[cmd.Id] = w
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.waiters, cmd.Id)
		s.mu.Unlock()
	}()
	select {
	case s.send <- &agentv1.ServerMessage{Body: &agentv1.ServerMessage_Command{Command: cmd}}:
	case <-s.done:
		return nil, ErrNotConnected
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case res := <-w:
		if !res.GetOk() {
			return res, fmt.Errorf("agent: %s", res.GetError())
		}
		return res, nil
	case <-s.done:
		return nil, ErrNotConnected
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
