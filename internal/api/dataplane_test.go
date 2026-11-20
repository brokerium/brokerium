// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func TestACLBinding(t *testing.T) {
	t.Parallel()
	acl := func(typ, name, op string) ACL {
		return ACL{ResourceType: typ, ResourceName: name, Principal: "User:alice", Operation: op, Permission: "ALLOW"}
	}
	tests := []struct {
		name     string
		acl      ACL
		typ      kmsg.ACLResourceType
		resource string
		wantErr  string
	}{
		{"topic", acl("TOPIC", "orders", "READ"), kmsg.ACLResourceTypeTopic, "orders", ""},
		{"group", acl("GROUP", "billing", "READ"), kmsg.ACLResourceTypeGroup, "billing", ""},
		{"cluster", acl("CLUSTER", "", "DESCRIBE"), kmsg.ACLResourceTypeCluster, "kafka-cluster", ""},
		{"transactional id", acl("TRANSACTIONAL_ID", "tx-1", "WRITE"), kmsg.ACLResourceTypeTransactionalId, "tx-1", ""},
		{"delegation token", acl("DELEGATION_TOKEN", "token-1", "DESCRIBE"), kmsg.ACLResourceTypeDelegationToken, "token-1", ""},
		{"user", acl("USER", "User:bob", "CREATE_TOKENS"), kmsg.ACLResourceTypeUser, "User:bob", ""},
		{"user in lowercase", acl("user", "User:bob", "describe_tokens"), kmsg.ACLResourceTypeUser, "User:bob", ""},
		{"unknown type", acl("BROKER", "1", "READ"), 0, "", `unsupported resource type "BROKER"`},
		{"any type", acl("ANY", "orders", "READ"), 0, "", `unsupported resource type "ANY"`},
		{"any operation", acl("TOPIC", "orders", "ANY"), 0, "", "invalid operation ANY"},
		{"unknown operation", acl("TOPIC", "orders", "FLY"), 0, "", "unable to parse"},
		{"match pattern", ACL{ResourceType: "TOPIC", ResourceName: "orders", PatternType: "MATCH", Principal: "User:alice",
			Operation: "READ", Permission: "ALLOW"}, 0, "", "patternType must be LITERAL or PREFIXED"},
		{"principal without its type", ACL{ResourceType: "TOPIC", ResourceName: "orders", Principal: "alice", Operation: "READ",
			Permission: "ALLOW"}, 0, "", "principal must look like User:<name>"},
		{"permission", ACL{ResourceType: "TOPIC", ResourceName: "orders", Principal: "User:alice", Operation: "READ",
			Permission: "MAYBE"}, 0, "", "permission must be ALLOW or DENY"},
	}
	for _, tt := range tests {
		b, err := aclBinding(tt.acl)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: aclBinding = %v, want %q", tt.name, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if b.ResourceType != tt.typ || b.ResourceName != tt.resource || b.ResourcePatternType != kmsg.ACLResourcePatternTypeLiteral ||
			b.Principal != "User:alice" || b.Host != "*" || b.PermissionType != kmsg.ACLPermissionTypeAllow {
			t.Errorf("%s: binding %+v", tt.name, b)
		}
		f := aclFilter(b)
		if f.ResourceType != b.ResourceType || *f.ResourceName != b.ResourceName || f.ResourcePatternType != b.ResourcePatternType ||
			*f.Principal != b.Principal || *f.Host != b.Host || f.Operation != b.Operation || f.PermissionType != b.PermissionType {
			t.Errorf("%s: filter %+v does not match the binding %+v exactly", tt.name, f, b)
		}
	}

	b, err := aclBinding(ACL{ResourceType: "USER", ResourceName: "User:", PatternType: "prefixed", Principal: "User:ops",
		Host: "10.0.0.1", Operation: "DESCRIBE_TOKENS", Permission: "deny"})
	if err != nil || b.ResourcePatternType != kmsg.ACLResourcePatternTypePrefixed || b.Host != "10.0.0.1" ||
		b.Operation != kmsg.ACLOperationDescribeTokens || b.PermissionType != kmsg.ACLPermissionTypeDeny {
		t.Errorf("prefixed USER binding %+v, %v", b, err)
	}
}

func TestACLError(t *testing.T) {
	t.Parallel()
	message := "ACL operation READ is not supported"
	err := aclError(kerr.InvalidRequest.Code, &message)
	if !errors.Is(err, kerr.InvalidRequest) || !strings.HasSuffix(err.Error(), ": "+message) {
		t.Errorf("aclError = %v", err)
	}
	if err := aclError(0, &message); err != nil {
		t.Errorf("success with a message: %v", err)
	}
	if err := aclError(kerr.SecurityDisabled.Code, nil); !errors.Is(err, kerr.SecurityDisabled) {
		t.Errorf("aclError without a message = %v", err)
	}
}
