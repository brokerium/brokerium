// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentgw

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/brokerium/brokerium/internal/agentauth"
	"github.com/brokerium/brokerium/internal/auth"
	"github.com/brokerium/brokerium/internal/store"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// memTokens keeps join tokens like the store: only unused, unexpired tokens are found and
// consumed.
type memTokens map[string]*store.JoinToken

func (m memTokens) active(hash string) (*store.JoinToken, bool) {
	t, ok := m[hash]
	return t, ok && t.UsedAt == nil && time.Now().Before(t.ExpiresAt)
}

func (m memTokens) FindJoinToken(_ context.Context, lookup []byte) (*store.JoinToken, error) {
	for hash := range m {
		if t, ok := m.active(hash); ok && bytes.Equal(agentauth.TokenLookup([]byte(hash)), lookup) {
			return t, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m memTokens) ConsumeJoinToken(_ context.Context, hash string) (*store.JoinToken, error) {
	t, ok := m.active(hash)
	if !ok {
		return nil, store.ErrNotFound
	}
	now := time.Now()
	t.UsedAt = &now
	return t, nil
}

func proofFor(t *testing.T, secret string) *agentv1.EnrollRequest {
	t.Helper()
	e, err := agentauth.NewEnrollment(agentauth.TokenKey(secret), "kafka-1", "0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	return e.Request
}

func TestRedeemProof(t *testing.T) {
	t.Parallel()
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	tests := []struct {
		name string
		// requests are redeemed in order; all but the last must succeed.
		requests func(t *testing.T, secret string) []*agentv1.EnrollRequest
		want     codes.Code
		consumed bool
	}{
		{"valid proof", func(t *testing.T, secret string) []*agentv1.EnrollRequest {
			return []*agentv1.EnrollRequest{proofFor(t, secret)}
		}, codes.OK, true},
		{"wrong token", func(t *testing.T, _ string) []*agentv1.EnrollRequest {
			return []*agentv1.EnrollRequest{proofFor(t, "another-secret")}
		}, codes.PermissionDenied, false},
		{"MAC with another key", func(t *testing.T, secret string) []*agentv1.EnrollRequest {
			req := proofFor(t, "another-secret")
			req.Proof.TokenLookup = agentauth.TokenLookup(agentauth.TokenKey(secret))
			return []*agentv1.EnrollRequest{req}
		}, codes.PermissionDenied, false},
		{"replayed after consumption", func(t *testing.T, secret string) []*agentv1.EnrollRequest {
			req := proofFor(t, secret)
			return []*agentv1.EnrollRequest{req, req}
		}, codes.PermissionDenied, true},
		{"second proof after consumption", func(t *testing.T, secret string) []*agentv1.EnrollRequest {
			return []*agentv1.EnrollRequest{proofFor(t, secret), proofFor(t, secret)}
		}, codes.PermissionDenied, true},
		{"malformed proof", func(t *testing.T, secret string) []*agentv1.EnrollRequest {
			req := proofFor(t, secret)
			req.Proof.Nonce = nil
			return []*agentv1.EnrollRequest{req}
		}, codes.InvalidArgument, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			secret, hash := auth.NewToken()
			tokens := memTokens{hash: {TokenHash: hash, NodeName: "kafka-1", ExpiresAt: time.Now().Add(time.Hour)}}
			reqs := tt.requests(t, secret)
			var err error
			for i, req := range reqs {
				var tok *store.JoinToken
				tok, err = redeemProof(context.Background(), tokens, discard, req)
				if i < len(reqs)-1 && err != nil {
					t.Fatalf("request %d: %v", i+1, err)
				}
				if err == nil && tok.NodeName != "kafka-1" {
					t.Fatalf("request %d redeemed the token of %q", i+1, tok.NodeName)
				}
			}
			if got := status.Code(err); got != tt.want {
				t.Errorf("redeemProof = %v, want %s", err, tt.want)
			}
			if consumed := tokens[hash].UsedAt != nil; consumed != tt.consumed {
				t.Errorf("token consumed = %v, want %v", consumed, tt.consumed)
			}
		})
	}
}

func TestRedeemProofExpired(t *testing.T) {
	t.Parallel()
	secret, hash := auth.NewToken()
	tokens := memTokens{hash: {TokenHash: hash, NodeName: "kafka-1", ExpiresAt: time.Now().Add(-time.Minute)}}
	_, err := redeemProof(context.Background(), tokens, slog.New(slog.NewTextHandler(io.Discard, nil)), proofFor(t, secret))
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("an expired token was redeemed: %v", err)
	}
}
