// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package kafka

import (
	"context"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"
)

// Kafka modes of brokers and clusters (ADR 0022).
const (
	ModeKRaft     = "kraft"
	ModeZooKeeper = "zookeeper"
	// ModeMigration is a broker with zookeeper.metadata.migration.enable, or a cluster whose
	// brokers do not all run in the same mode.
	ModeMigration = "migration"
)

// BrokerAPIs is what the ApiVersions response of one broker tells about it.
type BrokerAPIs struct {
	// Version is the release franz-go estimates from the protocol versions, such as "3.5",
	// "3.4 to 3.5" or "4.4 or later"; empty when the versions match no release.
	Version string
	// ZooKeeper is set when the broker offers the inter-broker APIs of ZooKeeper mode.
	ZooKeeper bool
}

// APIsOf reads an ApiVersions response.
func APIsOf(resp *kmsg.ApiVersionsResponse) BrokerAPIs {
	vs := kversion.FromApiVersionsResponse(resp)
	return BrokerAPIs{Version: estimate(vs.VersionGuess()), ZooKeeper: vs.HasKey(kmsg.LeaderAndISR.Int16())}
}

// estimate renders a kversion guess ("v3.5", "between v3.4 and v3.5", "at least v4.4", "unknown
// custom version at least v3.5") in the form of Broker.Version; other guesses say nothing.
func estimate(guess string) string {
	guess = strings.TrimPrefix(guess, "unknown custom version ")
	switch {
	case strings.HasPrefix(guess, "between "):
		lo, hi, ok := strings.Cut(strings.TrimPrefix(guess, "between "), " and ")
		if ok {
			return strings.TrimPrefix(lo, "v") + " to " + strings.TrimPrefix(hi, "v")
		}
	case strings.HasPrefix(guess, "at least "):
		return strings.TrimPrefix(guess, "at least v") + " or later"
	case strings.HasPrefix(guess, "v"):
		return strings.TrimPrefix(guess, "v")
	}
	return ""
}

// brokerAPIs sends ApiVersions to each broker; brokers that do not answer are missing.
func (c *Client) brokerAPIs(ctx context.Context, ids []int32) map[int32]BrokerAPIs {
	out := map[int32]BrokerAPIs{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			resp, err := pingRequest().RequestWith(ctx, c.KGO.Broker(int(id)))
			if err != nil || resp.ErrorCode != 0 {
				return
			}
			mu.Lock()
			out[id] = APIsOf(resp)
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}

// Release is a Kafka release number.
type Release struct{ Major, Minor, Patch int }

// Less reports whether r is older than o.
func (r Release) Less(o Release) bool {
	if r.Major != o.Major {
		return r.Major < o.Major
	}
	if r.Minor != o.Minor {
		return r.Minor < o.Minor
	}
	return r.Patch < o.Patch
}

// VersionRange are the releases a broker may run: one release when its agent reports it, all
// patches of one or more minor releases when it is estimated from the protocol versions.
type VersionRange struct{ Min, Max Release }

// parseRelease reads "4.3.1" or "4.3"; a minor release stands for its first patch, or for its
// last one when last is set.
func parseRelease(s string, last bool) (Release, bool) {
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Release{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Release{}, false
		}
		n[i] = v
	}
	if len(parts) == 2 && last {
		n[2] = math.MaxInt
	}
	return Release{n[0], n[1], n[2]}, true
}

// ParseVersion reads a Broker.Version: "4.3.1", "3.5", "3.4 to 3.5" or "4.4 or later".
func ParseVersion(v string) (VersionRange, bool) {
	if lo, ok := strings.CutSuffix(v, " or later"); ok {
		r, ok := parseRelease(lo, false)
		return VersionRange{Min: r, Max: Release{math.MaxInt, math.MaxInt, math.MaxInt}}, ok
	}
	lo, hi, ok := strings.Cut(v, " to ")
	if !ok {
		hi = lo
	}
	r1, ok1 := parseRelease(lo, false)
	r2, ok2 := parseRelease(hi, true)
	return VersionRange{Min: r1, Max: r2}, ok1 && ok2 && !r2.Less(r1)
}

// AtLeast reports whether every release of the range is major.minor or later.
func (r VersionRange) AtLeast(major, minor int) bool {
	return !r.Min.Less(Release{Major: major, Minor: minor})
}

// Below reports whether every release of the range is older than major.minor.
func (r VersionRange) Below(major, minor int) bool {
	return r.Max.Less(Release{Major: major, Minor: minor})
}

func (r VersionRange) overlaps(o VersionRange) bool {
	return !r.Max.Less(o.Min) && !o.Max.Less(r.Min)
}

// Summarize sets the mode of the cluster and whether its brokers run different releases from
// the modes and versions of the brokers. The mode is kraft when every broker runs KRaft,
// zookeeper when every broker runs ZooKeeper and migration otherwise; brokers whose mode is not
// known do not count.
func (s *Snapshot) Summarize() {
	modes := map[string]bool{}
	var ranges []VersionRange
	for _, b := range s.Brokers {
		if b.Mode != "" {
			modes[b.Mode] = true
		}
		if r, ok := ParseVersion(b.Version); ok {
			ranges = append(ranges, r)
		}
	}
	s.Mode = ""
	switch {
	case len(modes) == 1 && modes[ModeKRaft]:
		s.Mode = ModeKRaft
	case len(modes) == 1 && modes[ModeZooKeeper]:
		s.Mode = ModeZooKeeper
	case len(modes) > 0:
		s.Mode = ModeMigration
	}
	s.MixedVersions = false
	for i := range ranges {
		for j := i + 1; j < len(ranges); j++ {
			s.MixedVersions = s.MixedVersions || !ranges[i].overlaps(ranges[j])
		}
	}
}
