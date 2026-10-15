// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package cluster

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bvaliev/brokerium/internal/kafka"
)

// Health statuses.
const (
	HealthOK          = "ok"
	HealthWarning     = "warning"
	HealthCritical    = "critical"
	HealthUnreachable = "unreachable"
	HealthUnknown     = "unknown"
)

// Issue is one finding of the health evaluation.
type Issue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// Health summarizes the state of a cluster.
type Health struct {
	Status             string  `json:"status"`
	Issues             []Issue `json:"issues"`
	Brokers            int     `json:"brokers"`
	BrokersAlive       int     `json:"brokersAlive"`
	BrokersFenced      int     `json:"brokersFenced"`
	BrokersCordoned    int     `json:"brokersCordoned"`
	Topics             int     `json:"topics"`
	Partitions         int     `json:"partitions"`
	OfflinePartitions  int     `json:"offlinePartitions"`
	UnderReplicated    int     `json:"underReplicated"`
	UnderMinISR        int     `json:"underMinIsr"`
	AtMinISR           int     `json:"atMinIsr"`
	Reassigning        int     `json:"reassigning"`
	QuorumLeader       int32   `json:"quorumLeader"`
	Voters             int     `json:"voters"`
	VotersLagging      int     `json:"votersLagging"`
	DiskUsedPercentMax float64 `json:"diskUsedPercentMax"`
	MetadataVersion    string  `json:"metadataVersion"`
	KRaftVersion       int16   `json:"kraftVersion"`
}

// VoterLagThreshold is the metadata offset lag above which a voter counts as lagging.
const VoterLagThreshold = 5000

// Evaluate derives health from a snapshot.
func Evaluate(s *kafka.Snapshot, now time.Time) Health {
	h := Health{Status: HealthOK, QuorumLeader: -1, Issues: []Issue{}}
	add := func(sev, code, format string, args ...any) {
		h.Issues = append(h.Issues, Issue{Severity: sev, Code: code, Message: fmt.Sprintf(format, args...)})
	}

	adding := map[string]map[int32]bool{}
	for _, r := range s.Reassignments {
		if adding[r.Topic] == nil {
			adding[r.Topic] = map[int32]bool{}
		}
		adding[r.Topic][r.Partition] = true
	}
	h.Reassigning = len(s.Reassignments)

	registered := map[int32]bool{}
	for _, b := range s.Brokers {
		registered[b.ID] = true
		h.Brokers++
		if b.Alive {
			h.BrokersAlive++
		}
		if b.Fenced {
			h.BrokersFenced++
		}
		if b.Cordoned {
			h.BrokersCordoned++
		}
		if b.DiskTotalBytes > 0 {
			used := 100 * float64(b.DiskTotalBytes-b.DiskUsableBytes) / float64(b.DiskTotalBytes)
			if used > h.DiskUsedPercentMax {
				h.DiskUsedPercentMax = used
			}
		}
	}
	var fenced []string
	for _, b := range s.Brokers {
		if b.Fenced || !b.Alive {
			fenced = append(fenced, fmt.Sprint(b.ID))
		}
	}
	if len(fenced) > 0 {
		add(HealthWarning, "broker_fenced", "broker(s) %s registered but not serving (fenced or offline)", strings.Join(fenced, ", "))
	}
	if h.BrokersCordoned > 0 {
		add("info", "broker_cordoned", "%d broker(s) cordoned: no new replicas are placed there", h.BrokersCordoned)
	}
	if h.DiskUsedPercentMax >= 90 {
		add(HealthCritical, "disk_full", "a broker volume is %.0f%% full", h.DiskUsedPercentMax)
	} else if h.DiskUsedPercentMax >= 80 {
		add(HealthWarning, "disk_high", "a broker volume is %.0f%% full", h.DiskUsedPercentMax)
	}

	for _, t := range s.Topics {
		h.Topics++
		for _, p := range t.Partitions {
			h.Partitions++
			if p.Leader < 0 {
				h.OfflinePartitions++
				continue
			}
			if len(p.ISR) < len(p.Replicas) && !adding[t.Name][p.ID] {
				h.UnderReplicated++
			}
			if len(p.ISR) < t.MinISR {
				h.UnderMinISR++
			} else if len(p.ISR) == t.MinISR && len(p.Replicas) > t.MinISR {
				h.AtMinISR++
			}
		}
	}
	if h.OfflinePartitions > 0 {
		add(HealthCritical, "offline_partitions", "%d partition(s) have no leader", h.OfflinePartitions)
	}
	if h.UnderMinISR > 0 {
		add(HealthCritical, "under_min_isr", "%d partition(s) are below min.insync.replicas; acks=all writes fail", h.UnderMinISR)
	}
	if h.UnderReplicated > 0 {
		add(HealthWarning, "under_replicated", "%d partition(s) are under-replicated", h.UnderReplicated)
	}
	if h.Reassigning > 0 {
		add("info", "reassigning", "%d partition reassignment(s) in progress", h.Reassigning)
	}

	if q := s.Quorum; q != nil {
		h.QuorumLeader = q.LeaderID
		h.Voters = len(q.Voters)
		for _, v := range q.Voters {
			stale := v.LastCaughtUpTimestamp > 0 && now.Sub(time.UnixMilli(v.LastCaughtUpTimestamp)) > 30*time.Second
			if v.Role != "leader" && (v.Lag > VoterLagThreshold || stale) {
				h.VotersLagging++
			}
		}
		if q.LeaderID < 0 {
			add(HealthCritical, "no_quorum_leader", "the controller quorum has no leader")
		}
		if h.Voters > 0 && h.VotersLagging >= (h.Voters+1)/2 {
			add(HealthCritical, "quorum_at_risk", "%d of %d controller voters are lagging", h.VotersLagging, h.Voters)
		} else if h.VotersLagging > 0 {
			add(HealthWarning, "voter_lagging", "%d controller voter(s) are lagging", h.VotersLagging)
		}
		if h.Voters%2 == 0 && h.Voters > 0 {
			add("info", "even_voters", "%d voters tolerate as many failures as %d", h.Voters, h.Voters-1)
		}
		var stale []string
		for _, o := range q.Observers {
			if registered[o.NodeID] {
				continue
			}
			if o.LastFetchTimestamp > 0 && now.Sub(time.UnixMilli(o.LastFetchTimestamp)) > time.Minute {
				stale = append(stale, fmt.Sprint(o.NodeID))
			}
		}
		if len(stale) > 0 {
			add("info", "stale_observers", "node(s) %s stopped fetching metadata (removed controllers or stopped nodes)", strings.Join(stale, ", "))
		}
	} else if s.Has(kafka.PartQuorum) {
		add(HealthWarning, "quorum_unknown", "the controller quorum could not be described")
	}

	for _, f := range s.Features {
		switch f.Name {
		case "metadata.version":
			h.MetadataVersion = MetadataVersionName(f.Finalized)
		case "kraft.version":
			h.KRaftVersion = f.Finalized
		}
	}
	if len(s.Errors) > 0 {
		add(HealthWarning, "partial_snapshot", "some information could not be collected: %s", strings.Join(s.Errors, "; "))
	}

	for _, i := range h.Issues {
		switch {
		case i.Severity == HealthCritical:
			h.Status = HealthCritical
		case i.Severity == HealthWarning && h.Status == HealthOK:
			h.Status = HealthWarning
		}
	}
	sort.SliceStable(h.Issues, func(i, j int) bool { return severityRank(h.Issues[i].Severity) > severityRank(h.Issues[j].Severity) })
	return h
}

func severityRank(s string) int {
	switch s {
	case HealthCritical:
		return 3
	case HealthWarning:
		return 2
	default:
		return 1
	}
}

// metadataVersions maps metadata.version feature levels to release names, taken from
// MetadataVersion.java on the Kafka 4.4 branch.
var metadataVersions = map[int16]string{
	7: "3.3-IV3", 8: "3.4-IV0", 9: "3.5-IV0", 10: "3.5-IV1", 11: "3.5-IV2", 12: "3.6-IV0", 13: "3.6-IV1",
	14: "3.6-IV2", 15: "3.7-IV0", 16: "3.7-IV1", 17: "3.7-IV2", 18: "3.7-IV3", 19: "3.7-IV4", 20: "3.8-IV0",
	21: "3.9-IV0", 22: "4.0-IV0", 23: "4.0-IV1", 24: "4.0-IV2", 25: "4.0-IV3", 26: "4.1-IV0", 27: "4.1-IV1",
	28: "4.2-IV0", 29: "4.2-IV1", 30: "4.3-IV0", 31: "4.4-IV0", 32: "4.4-IV1", 33: "4.4-IV2", 34: "4.5-IV0",
}

// MetadataVersionName renders a metadata.version level; unknown levels are shown as numbers.
func MetadataVersionName(level int16) string {
	if name, ok := metadataVersions[level]; ok {
		return name
	}
	return fmt.Sprintf("level %d", level)
}
