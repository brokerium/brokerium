// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package kafka

import (
	"math"
	"testing"

	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"
)

// apiVersions is the ApiVersions response of a broker that supports the given request versions.
func apiVersions(vs *kversion.Versions) *kmsg.ApiVersionsResponse {
	resp := kmsg.NewPtrApiVersionsResponse()
	vs.EachMaxKeyVersion(func(k, v int16) {
		key := kmsg.NewApiVersionsResponseApiKey()
		key.ApiKey, key.MaxVersion = k, v
		resp.ApiKeys = append(resp.ApiKeys, key)
	})
	return resp
}

func TestAPIsOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		vs   *kversion.Versions
		want BrokerAPIs
	}{
		{"ZooKeeper broker on 3.5", kversion.V3_5_0(), BrokerAPIs{Version: "3.5", ZooKeeper: true}},
		{"ZooKeeper broker on 3.9", kversion.V3_9_0(), BrokerAPIs{Version: "3.9", ZooKeeper: true}},
		{"KRaft broker on 4.0", kversion.V4_0_0(), BrokerAPIs{Version: "4.0"}},
		{"KRaft broker on 4.4", kversion.V4_4_0(), BrokerAPIs{Version: "4.4"}},
	}
	for _, tc := range tests {
		if got := APIsOf(apiVersions(tc.vs)); got != tc.want {
			t.Errorf("%s: APIsOf = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestEstimate(t *testing.T) {
	t.Parallel()
	for guess, want := range map[string]string{
		"v3.5":                                 "3.5",
		"between v3.4 and v3.5":                "3.4 to 3.5",
		"at least v4.4":                        "4.4 or later",
		"unknown custom version at least v3.5": "3.5 or later",
		"unknown custom version":               "",
		"not even v0.8.0":                      "",
	} {
		if got := estimate(guess); got != want {
			t.Errorf("estimate(%q) = %q, want %q", guess, got, want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	t.Parallel()
	last := math.MaxInt
	tests := []struct {
		in   string
		want VersionRange
		ok   bool
	}{
		{"4.3.1", VersionRange{Release{4, 3, 1}, Release{4, 3, 1}}, true},
		{"3.5", VersionRange{Release{3, 5, 0}, Release{3, 5, last}}, true},
		{"3.4 to 3.5", VersionRange{Release{3, 4, 0}, Release{3, 5, last}}, true},
		{"4.4 or later", VersionRange{Release{4, 4, 0}, Release{last, last, last}}, true},
		{"", VersionRange{}, false},
		{"4", VersionRange{}, false},
		{"4.3.1.2", VersionRange{}, false},
		{"4.x", VersionRange{}, false},
		{"4.3 to 4.2", VersionRange{}, false},
		{"latest", VersionRange{}, false},
	}
	for _, tc := range tests {
		got, ok := ParseVersion(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("ParseVersion(%q) = %+v, %v; want %+v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestVersionRangeBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version      string
		major, minor int
		wantAtLeast  bool
		wantBelow    bool
	}{
		{version: "4.3", major: 4, minor: 3, wantAtLeast: true},
		{version: "4.3.0", major: 4, minor: 3, wantAtLeast: true},
		{version: "4.2.9", major: 4, minor: 3, wantBelow: true},
		{version: "4.2 to 4.3", major: 4, minor: 3},
		{version: "4.4 or later", major: 4, minor: 3, wantAtLeast: true},
		{version: "3.4", major: 3, minor: 5, wantBelow: true},
		{version: "3.4 to 3.5", major: 3, minor: 5},
	}
	for _, tc := range tests {
		r, ok := ParseVersion(tc.version)
		if !ok {
			t.Fatalf("ParseVersion(%q) failed", tc.version)
		}
		if got := r.AtLeast(tc.major, tc.minor); got != tc.wantAtLeast {
			t.Errorf("%s AtLeast(%d, %d) = %v", tc.version, tc.major, tc.minor, got)
		}
		if got := r.Below(tc.major, tc.minor); got != tc.wantBelow {
			t.Errorf("%s Below(%d, %d) = %v", tc.version, tc.major, tc.minor, got)
		}
	}
}

func TestSummarize(t *testing.T) {
	t.Parallel()
	broker := func(mode, version string) Broker { return Broker{Mode: mode, Version: version} }
	tests := []struct {
		name    string
		brokers []Broker
		mode    string
		mixed   bool
	}{
		{"KRaft", []Broker{broker(ModeKRaft, "4.3"), broker(ModeKRaft, "4.3")}, ModeKRaft, false},
		{"ZooKeeper", []Broker{broker(ModeZooKeeper, "3.5"), broker(ModeZooKeeper, "3.5")}, ModeZooKeeper, false},
		{"KRaft and ZooKeeper brokers", []Broker{broker(ModeKRaft, "3.9"), broker(ModeZooKeeper, "3.9")}, ModeMigration, false},
		{"one broker with the migration flag", []Broker{broker(ModeZooKeeper, "3.9"), broker(ModeMigration, "3.9")}, ModeMigration, false},
		{"brokers of unknown mode do not count", []Broker{broker(ModeZooKeeper, "3.5"), broker("", "")}, ModeZooKeeper, false},
		{"nothing known", []Broker{broker("", "")}, "", false},
		{"two minor releases", []Broker{broker(ModeKRaft, "3.9"), broker(ModeKRaft, "4.0")}, ModeKRaft, true},
		{"two patches", []Broker{broker(ModeKRaft, "4.3.1"), broker(ModeKRaft, "4.3.2")}, ModeKRaft, true},
		{"an exact version within an estimate", []Broker{broker(ModeKRaft, "3.5"), broker(ModeKRaft, "3.5.2")}, ModeKRaft, false},
		{"an estimate that does not decide", []Broker{broker(ModeKRaft, "3.4 to 3.5"), broker(ModeKRaft, "3.5")}, ModeKRaft, false},
	}
	for _, tc := range tests {
		s := &Snapshot{Brokers: tc.brokers}
		s.Summarize()
		if s.Mode != tc.mode || s.MixedVersions != tc.mixed {
			t.Errorf("%s: mode %q, mixed %v; want %q, %v", tc.name, s.Mode, s.MixedVersions, tc.mode, tc.mixed)
		}
	}
}
