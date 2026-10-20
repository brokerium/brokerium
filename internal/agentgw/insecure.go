// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentgw

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/brokerium/brokerium/internal/agentauth"
	"github.com/brokerium/brokerium/internal/store"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// ServeInsecure serves the insecure transport (ADR 0017) on addr until ctx ends: plaintext gRPC
// (HTTP/2 with prior knowledge) for reverse proxies that terminate TLS. Enrollment and sessions
// authenticate every message instead of the connection.
func (g *Gateway) ServeInsecure(ctx context.Context, addr string) error {
	srv := grpc.NewServer(keepaliveOptions()...)
	agentv1.RegisterAgentServiceServer(srv, &insecureService{g: g})
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()
	g.Log.Info("insecure agent listener", "addr", addr)
	return srv.Serve(lis)
}

// insecureService is the agent service of the insecure listener. RenewCertificate stays
// unimplemented: nodes of the insecure transport have no certificate.
type insecureService struct {
	agentv1.UnimplementedAgentServiceServer
	g *Gateway
}

func (s *insecureService) Enroll(ctx context.Context, req *agentv1.EnrollRequest) (*agentv1.EnrollResponse, error) {
	return s.g.enrollInsecure(ctx, req)
}

func (s *insecureService) Connect(stream agentv1.AgentService_ConnectServer) error {
	return s.g.connectInsecure(stream)
}

// joinTokens is the part of the store that redeems enrollment proofs.
type joinTokens interface {
	FindJoinToken(ctx context.Context, lookup []byte) (*store.JoinToken, error)
	ConsumeJoinToken(ctx context.Context, tokenHash string) (*store.JoinToken, error)
}

// redeemProof consumes the join token an enrollment request proves. A proof that does not match
// leaves the token valid.
func redeemProof(ctx context.Context, tokens joinTokens, log *slog.Logger, req *agentv1.EnrollRequest) (*store.JoinToken, error) {
	if err := agentauth.CheckProof(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	denied := status.Error(codes.PermissionDenied, "join token is invalid, used or expired")
	tok, err := tokens.FindJoinToken(ctx, req.GetProof().GetTokenLookup())
	if errors.Is(err, store.ErrNotFound) {
		return nil, denied
	} else if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := agentauth.VerifyProof([]byte(tok.TokenHash), req); err != nil {
		log.Warn("rejected enrollment", "node", tok.NodeName, "hostname", req.GetHostname(), "error", err)
		return nil, denied
	}
	if tok, err = tokens.ConsumeJoinToken(ctx, tok.TokenHash); err != nil {
		return nil, denied
	}
	return tok, nil
}

// enrollInsecure exchanges an enrollment proof for a node secret, encrypted to the key of the
// agent and signed with the CA key.
func (g *Gateway) enrollInsecure(ctx context.Context, req *agentv1.EnrollRequest) (*agentv1.EnrollResponse, error) {
	if req.GetToken() != "" || len(req.GetCsrDer()) > 0 {
		return nil, status.Error(codes.InvalidArgument,
			"this listener accepts enrollment proofs only; run the agent with BROKERIUM_AGENT_TRANSPORT=insecure")
	}
	tok, err := redeemProof(ctx, g.Store, g.Log, req)
	if err != nil {
		return nil, err
	}
	node, err := g.enrolledNode(ctx, tok)
	if err != nil {
		return nil, err
	}
	secret := make([]byte, agentauth.SecretSize)
	_, _ = rand.Read(secret)
	resp := &agentv1.EnrollResponse{NodeId: node.ID, NodeName: node.Name, CaCertDer: g.CA.DER}
	if err := agentauth.SealEnrollment(g.CA.Key, req, resp, secret); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	sealed, err := g.Box.Seal(secret)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := g.Store.UpdateNodeInsecureEnrollment(ctx, node.ID, sealed, req.GetHostname(), req.GetAgentVersion()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	g.Log.Info("agent enrolled", "node", node.Name, "id", node.ID, "hostname", req.GetHostname(), "transport", store.TransportInsecure)
	return resp, nil
}

// handshakeTimeout bounds the session handshake, so streams that never authenticate do not
// stay open.
const handshakeTimeout = 30 * time.Second

// recvBefore receives the next message with recv, or fails once deadline passes. Recv of a
// gRPC stream returns when the handler does, which ends the goroutine.
func recvBefore[T any](recv func() (T, error), deadline time.Time) (T, error) {
	type result struct {
		m   T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		m, err := recv()
		ch <- result{m, err}
	}()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.m, r.err
	case <-timer.C:
		var zero T
		return zero, status.Error(codes.DeadlineExceeded, "the session handshake took too long")
	}
}

// connectInsecure runs a Connect stream of the insecure transport: the session handshake, then
// signed commands and agent messages authenticated with the node secret. The session replaces
// an earlier one of the node only after the first authenticated agent message.
func (g *Gateway) connectInsecure(stream agentv1.AgentService_ConnectServer) error {
	deadline := time.Now().Add(handshakeTimeout)
	in, err := recvBefore(stream.Recv, deadline)
	if err != nil {
		return err
	}
	hello := in.GetSessionHello()
	if hello == nil {
		return status.Error(codes.InvalidArgument,
			"the stream must start with a session hello; run the agent with BROKERIUM_AGENT_TRANSPORT=insecure")
	}
	node, secret, err := g.nodeSecret(stream.Context(), hello.GetNodeId())
	if err != nil {
		return err
	}
	sess, accept, err := agentauth.Accept(g.CA.Key, secret, hello, time.Now())
	if err != nil {
		g.Log.Warn("rejected agent session", "node", node.Name, "error", err)
		return status.Error(codes.PermissionDenied, err.Error())
	}
	if err := stream.Send(&agentv1.ServerMessage{Body: &agentv1.ServerMessage_SessionAccept{SessionAccept: accept}}); err != nil {
		return err
	}
	signed := &signedStream{AgentService_ConnectServer: stream, session: sess, log: g.Log, node: node.Name}
	first, err := recvBefore(signed.Recv, deadline)
	if err != nil {
		return err
	}
	if first.GetHello() == nil {
		g.Log.Warn("rejected agent session", "node", node.Name, "error", "the session does not start with the agent hello")
		return status.Error(codes.InvalidArgument, "the session must start with the agent hello")
	}
	return g.serve(node, signed, first)
}

// nodeSecret returns a node of the insecure transport and its node secret.
func (g *Gateway) nodeSecret(ctx context.Context, id string) (*store.Node, []byte, error) {
	node, err := g.Store.GetNode(ctx, id)
	if err != nil {
		g.Log.Warn("rejected agent session", "nodeId", id, "error", "unknown node")
		return nil, nil, status.Error(codes.PermissionDenied, "unknown node")
	}
	if node.Transport != store.TransportInsecure {
		g.Log.Warn("rejected agent session", "node", node.Name, "error", "the node is not enrolled with the insecure transport")
		return nil, nil, status.Error(codes.PermissionDenied, "the node is not enrolled with the insecure transport")
	}
	sealed, err := g.Store.NodeSecret(ctx, node.ID)
	if err != nil {
		return nil, nil, status.Error(codes.Internal, err.Error())
	}
	secret, err := g.Box.Open(sealed)
	if err != nil {
		return nil, nil, status.Error(codes.Internal, "decrypting the node secret: "+err.Error())
	}
	return node, secret, nil
}

// signedStream is the server side of a Connect stream of the insecure transport: it signs every
// command and verifies every agent message before the session uses it.
type signedStream struct {
	agentv1.AgentService_ConnectServer
	session *agentauth.ServerSession
	log     *slog.Logger
	node    string
}

func (s *signedStream) Send(m *agentv1.ServerMessage) error {
	sealed, err := s.session.Seal(m)
	if err != nil {
		return err
	}
	return s.AgentService_ConnectServer.Send(sealed)
}

func (s *signedStream) Recv() (*agentv1.AgentMessage, error) {
	in, err := s.AgentService_ConnectServer.Recv()
	if err != nil {
		return nil, err
	}
	m, err := s.session.Open(in)
	if err != nil {
		s.log.Warn("rejected agent message, closing the session", "node", s.node, "error", err)
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	return m, nil
}
