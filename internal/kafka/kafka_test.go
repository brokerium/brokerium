// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package kafka

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/twmb/franz-go/pkg/kerr"

	"github.com/bvaliev/brokerium/internal/store"
)

func TestUUID(t *testing.T) {
	t.Parallel()
	id := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	s := EncodeUUID(id)
	got, err := DecodeUUID(s)
	if err != nil || got != id {
		t.Fatalf("DecodeUUID(%q) = %v, %v", s, got, err)
	}
	if EncodeUUID([16]byte{}) != "" {
		t.Error("the zero UUID must encode as empty")
	}
	// A directory id printed by kafka-storage.sh.
	if _, err := DecodeUUID("tQZ2Eap5SyKCiBmkOuSVBQ"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"", "short", "tQZ2Eap5SyKCiBmkOuSVBQ==", "!!!!!!!!!!!!!!!!!!!!!!"} {
		if _, err := DecodeUUID(bad); err == nil {
			t.Errorf("DecodeUUID(%q) accepted", bad)
		}
	}
}

func TestBuildThrottle(t *testing.T) {
	t.Parallel()
	moves := []Assignment{
		{Topic: "a", Partition: 0, Replicas: []int32{1, 2, 4}},
		{Topic: "a", Partition: 1, Replicas: []int32{4, 5, 2}},
	}
	current := map[string][]int32{"a-0": {1, 2, 3}, "a-1": {3, 1, 2}}
	alive := map[int32]bool{1: true, 2: true, 4: true, 5: true} // broker 3 is dead
	spec := BuildThrottle(10, moves, current, alive)

	want := ThrottleSpec{
		RateBytesPerSec: 10,
		Brokers:         []int32{1, 2, 4, 5},
		LeaderReplicas:  map[string][]string{"a": {"0:1", "0:2", "1:1", "1:2"}},
		FollowerReplica: map[string][]string{"a": {"0:4", "1:4", "1:5"}},
	}
	if !reflect.DeepEqual(spec, want) {
		t.Errorf("BuildThrottle =\n%+v\nwant\n%+v", spec, want)
	}
}

func TestTryNextController(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		want bool
	}{
		{errors.New("dial tcp: connection refused"), true},
		{kerr.NotLeaderForPartition, true},
		{fmt.Errorf("describe quorum: %w", kerr.NotController), true},
		{kerr.ClusterAuthorizationFailed, false},
		{kerr.UnsupportedVersion, false},
	}
	for _, tc := range tests {
		if got := tryNextController(tc.err); got != tc.want {
			t.Errorf("tryNextController(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestNewClientValidatesProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		p    Profile
		ok   bool
	}{
		{"no servers", Profile{}, false},
		{"plaintext", Profile{BootstrapServers: []string{"localhost:9092"}}, true},
		{"scram", Profile{BootstrapServers: []string{"b:9092"},
			Security: store.Security{Protocol: "SASL_SSL", SASLMechanism: "SCRAM-SHA-512", Username: "u"},
			Secrets:  store.Secrets{Password: "p"}}, true},
		{"msk iam", Profile{BootstrapServers: []string{"b:9098"},
			Security: store.Security{Protocol: "SASL_SSL", SASLMechanism: "AWS_MSK_IAM", AWSRegion: "eu-west-1"}}, true},
		{"unknown mechanism", Profile{BootstrapServers: []string{"b:9092"},
			Security: store.Security{Protocol: "SASL_PLAINTEXT", SASLMechanism: "GSSAPI"}}, false},
		{"bad CA", Profile{BootstrapServers: []string{"b:9093"},
			Security: store.Security{Protocol: "SSL", TLSCACertPEM: "not a pem"}}, false},
		{"half a client cert", Profile{BootstrapServers: []string{"b:9093"},
			Security: store.Security{Protocol: "SSL"}, Secrets: store.Secrets{TLSClientCertPEM: "x"}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := NewClient(tc.p)
			if (err == nil) != tc.ok {
				t.Fatalf("NewClient error = %v, want ok=%v", err, tc.ok)
			}
			if c != nil {
				c.Close()
			}
		})
	}
}
