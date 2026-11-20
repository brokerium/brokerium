// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package problems

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/brokerium/brokerium/internal/balancer"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/store"
)

// Input is what the rules read.
type Input struct {
	View *cluster.View
	// Nodes are the agent nodes assigned to the cluster.
	Nodes []*store.Node
	Now   time.Time
}

// Thresholds of the rules, documented in docs/guide/problems.md.
const (
	// targetReplicas is the replication factor fixes raise topics to, and the one internal topics
	// need on clusters with at least as many brokers.
	targetReplicas = 3
	// A broker is skewed with more replicas than replicaSkewRatio times the mean and at least
	// replicaSkewMin replicas above the mean.
	replicaSkewRatio = 1.3
	replicaSkewMin   = 10
	// A broker is skewed with more data than dataSkewRatio times the mean and more than
	// dataSkewMin bytes.
	dataSkewRatio = 1.5
	dataSkewMin   = 1 << 30
	// agentSilence is how long an agent may go unseen before it counts as gone.
	agentSilence = 24 * time.Hour
)

// internalTopics are the internal topics of Kafka with what fails when a partition of one is
// unavailable, and the broker setting with which Kafka creates them.
var internalTopics = map[string]struct{ loss, setting string }{
	"__consumer_offsets": {"consumer groups coordinated on that partition cannot commit or fetch their offsets",
		"offsets.topic.replication.factor"},
	"__transaction_state": {"producers whose transactions are coordinated on that partition cannot use transactions",
		"transaction.state.log.replication.factor"},
	"__share_group_state": {"share groups whose state is on that partition stop delivering records",
		"share.coordinator.state.topic.replication.factor"},
}

type rule func(in *Input, add func(Finding))

var rules = []rule{topicReplication, consumerOffsetsPartitions, brokersWithoutReplicas, replicaSkew, dataSkew,
	agentsNotSeen, healthIssues}

// Evaluate runs the rules on the input and returns the findings, the most severe first.
func Evaluate(in *Input) []Finding {
	out := []Finding{}
	seen := map[string]bool{}
	add := func(f Finding) {
		f.Fingerprint = Fingerprint(f.Code, f.Object)
		if seen[f.Fingerprint] {
			return
		}
		seen[f.Fingerprint] = true
		if f.Evidence == nil {
			f.Evidence = []Evidence{}
		}
		if f.Fixes == nil {
			f.Fixes = []Fix{}
		}
		out = append(out, f)
	}
	for _, r := range rules {
		r(in, add)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if severityRank(a.Severity) != severityRank(b.Severity) {
			return severityRank(a.Severity) < severityRank(b.Severity)
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Object.ID < b.Object.ID
	})
	return out
}

func serving(b kafka.Broker) bool { return b.Alive && !b.Fenced }

// placeable lists the brokers that can take new replicas: serving and not cordoned.
func placeable(s *kafka.Snapshot) []kafka.Broker {
	var out []kafka.Broker
	for _, b := range s.Brokers {
		if serving(b) && !b.Cordoned {
			out = append(out, b)
		}
	}
	return out
}

func servingCount(s *kafka.Snapshot) int {
	n := 0
	for _, b := range s.Brokers {
		if serving(b) {
			n++
		}
	}
	return n
}

// replicationFactor is the smallest number of replicas of a partition of the topic.
func replicationFactor(t kafka.Topic) int {
	rf := 0
	for i, p := range t.Partitions {
		if i == 0 || len(p.Replicas) < rf {
			rf = len(p.Replicas)
		}
	}
	return rf
}

// minInsyncReplicas returns the effective min.insync.replicas of a topic: its own value or the
// broker default, as DescribeConfigs reports it. The snapshot assumes 1 when it could not read
// the configs, so the value counts only when the configs have it.
func minInsyncReplicas(t kafka.Topic) (int, bool) {
	v, ok := t.Configs["min.insync.replicas"]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

// raiseReplication fixes a topic by raising its replication factor to target, when the cluster
// can reassign partitions.
func raiseReplication(caps cluster.Capabilities, topic string, rf, target int) []Fix {
	if !caps.Reassign || target <= rf {
		return nil
	}
	return []Fix{{Operation: OpChangeReplicationFactor, Label: fmt.Sprintf("Raise the replication factor to %d", target),
		Params: ReplicationParams{Topics: []string{topic}, ReplicationFactor: target}}}
}

// setMinInsyncReplicas fixes a topic by changing its min.insync.replicas.
func setMinInsyncReplicas(caps cluster.Capabilities, topic string, value int) []Fix {
	if !caps.ManageConfigs {
		return nil
	}
	return []Fix{{Operation: OpAlterTopicConfigs, Label: fmt.Sprintf("Set min.insync.replicas to %d", value),
		Params: TopicConfigParams{Topics: []string{topic}, Set: map[string]string{"min.insync.replicas": strconv.Itoa(value)}}}}
}

// rebalance fixes the placement of replicas with the rebalance operation.
func rebalance(caps cluster.Capabilities) []Fix {
	if !caps.Reassign {
		return nil
	}
	return []Fix{{Operation: "rebalance", Label: "Rebalance the cluster", Params: rebalanceParams{Mode: balancer.ModeEven}}}
}

type rebalanceParams struct {
	Mode string `json:"mode"`
}

// topicReplication checks the replication factor of every topic against min.insync.replicas
// and the brokers. A topic gets at most one of these findings, the most severe that applies.
func topicReplication(in *Input, add func(Finding)) {
	s := in.View.Snapshot
	if s == nil {
		return
	}
	caps := in.View.Capabilities
	alive := servingCount(s)
	room := len(placeable(s))
	for _, t := range s.Topics {
		if t.Error != "" || len(t.Partitions) == 0 {
			continue
		}
		rf := replicationFactor(t)
		minISR, known := minInsyncReplicas(t)
		target := targetReplicas
		if known {
			target = max(target, minISR+1)
		}
		target = min(target, room)
		raise := raiseReplication(caps, t.Name, rf, target)
		evidence := []Evidence{{"replication factor", strconv.Itoa(rf)}}
		if known {
			evidence = append(evidence, Evidence{"min.insync.replicas", strconv.Itoa(minISR)})
		}
		evidence = append(evidence, Evidence{"partitions", strconv.Itoa(len(t.Partitions))})
		obj := Object{Kind: ObjectTopic, ID: t.Name}
		internal, isInternal := internalTopics[t.Name]
		switch {
		case known && rf < minISR:
			add(Finding{Code: "topic-rf-below-min-isr", Severity: SeverityCritical, Object: obj,
				Title: "Replication factor below min.insync.replicas",
				Explanation: fmt.Sprintf("Topic %s keeps %d replica(s) of each partition, but min.insync.replicas is %d. "+
					"A partition never has that many in-sync replicas, so every write of a producer with acks=all fails "+
					"with NOT_ENOUGH_REPLICAS.", t.Name, rf, minISR),
				Evidence:       evidence,
				Recommendation: "Raise the replication factor above min.insync.replicas, or lower min.insync.replicas to fit the replication factor.",
				Fixes:          append(raise, setMinInsyncReplicas(caps, t.Name, max(1, rf-1))...)})
		case isInternal && len(s.Brokers) >= targetReplicas && rf < targetReplicas:
			add(Finding{Code: "internal-topic-rf-low", Severity: SeverityWarning, Object: obj,
				Title: "Internal topic with too few replicas",
				Explanation: fmt.Sprintf("The internal topic %s has replication factor %d on a cluster with %d brokers. "+
					"When the brokers that hold one of its partitions fail, %s.", t.Name, rf, len(s.Brokers), internal.loss),
				Evidence: append(evidence, Evidence{"brokers", strconv.Itoa(len(s.Brokers))}),
				Recommendation: fmt.Sprintf("Raise the replication factor of %s to %d, and set %s=%d in server.properties, "+
					"which Kafka uses when it creates the topic.", t.Name, targetReplicas, internal.setting, targetReplicas),
				Fixes: raise})
		case rf == 1 && alive > 1:
			add(Finding{Code: "topic-rf-one", Severity: SeverityWarning, Object: obj,
				Title: "Topic with a single replica",
				Explanation: fmt.Sprintf("Topic %s keeps one copy of each partition on a cluster with %d serving brokers. "+
					"When the broker of a partition stops, the partition is offline, and when its disk fails, the records "+
					"are lost.", t.Name, alive),
				Evidence:       evidence,
				Recommendation: "Raise the replication factor to 3, or to the number of brokers on smaller clusters.",
				Fixes:          raise})
		case known && rf == minISR && rf > 1:
			add(Finding{Code: "topic-rf-equals-min-isr", Severity: SeverityWarning, Object: obj,
				Title: "Replication factor equals min.insync.replicas",
				Explanation: fmt.Sprintf("Topic %s has replication factor %d and min.insync.replicas %d. While one broker "+
					"of a partition restarts or fails, the partition has fewer in-sync replicas than required, and producers "+
					"with acks=all fail until the broker is back; a rolling restart stops their writes broker by broker.",
					t.Name, rf, minISR),
				Evidence:       evidence,
				Recommendation: "Keep the replication factor at least one above min.insync.replicas, for example 3 with min.insync.replicas 2.",
				Fixes:          append(raise, setMinInsyncReplicas(caps, t.Name, rf-1)...)})
		case known && minISR == 1 && rf >= 2:
			fixes := raiseReplication(caps, t.Name, rf, min(targetReplicas, room))
			if rf >= targetReplicas {
				fixes = setMinInsyncReplicas(caps, t.Name, 2)
			}
			add(Finding{Code: "topic-min-isr-one", Severity: SeverityInfo, Object: obj,
				Title: "min.insync.replicas is 1",
				Explanation: fmt.Sprintf("Topic %s has replication factor %d but min.insync.replicas 1, so a write with "+
					"acks=all is acknowledged as soon as the leader has it. When the leader fails before a follower copied "+
					"the write, the write is lost.", t.Name, rf),
				Evidence:       evidence,
				Recommendation: "Use min.insync.replicas 2 with replication factor 3, so that acknowledged writes survive the failure of a broker.",
				Fixes:          fixes})
		}
	}
}

// consumerOffsetsPartitions finds a __consumer_offsets topic with fewer partitions than brokers.
func consumerOffsetsPartitions(in *Input, add func(Finding)) {
	s := in.View.Snapshot
	if s == nil {
		return
	}
	for _, t := range s.Topics {
		if t.Name != "__consumer_offsets" || len(t.Partitions) == 0 || len(t.Partitions) >= len(s.Brokers) {
			continue
		}
		add(Finding{Code: "consumer-offsets-partitions", Severity: SeverityInfo, Object: Object{Kind: ObjectTopic, ID: t.Name},
			Title: "__consumer_offsets has fewer partitions than brokers",
			Explanation: fmt.Sprintf("Kafka coordinates a consumer group on the leader of the __consumer_offsets partition "+
				"that its group id hashes to. With %d partitions and %d brokers, at most %d brokers coordinate groups, and "+
				"the others take none of that load. Kafka cannot change the number of partitions of __consumer_offsets: "+
				"offsets.topic.num.partitions applies only when the topic is created, and more partitions would map groups "+
				"to partitions that do not hold their committed offsets.", len(t.Partitions), len(s.Brokers), len(t.Partitions)),
			Evidence: []Evidence{{"partitions", strconv.Itoa(len(t.Partitions))}, {"brokers", strconv.Itoa(len(s.Brokers))}},
			Recommendation: "Nothing can be changed on this cluster. For new clusters, keep offsets.topic.num.partitions " +
				"(50 by default) at least as high as the number of brokers."})
	}
}

// brokersWithoutReplicas finds serving brokers without replicas on clusters that have enough
// replicas to give every broker some. Reassignments in progress move replicas, so the rule
// waits for them.
func brokersWithoutReplicas(in *Input, add func(Finding)) {
	s := in.View.Snapshot
	if s == nil || len(s.Reassignments) > 0 || !in.View.Capabilities.Reassign {
		return
	}
	brokers := placeable(s)
	total := 0
	for _, b := range s.Brokers {
		total += b.Replicas
	}
	if total < len(brokers) {
		return
	}
	for _, b := range brokers {
		if b.Replicas > 0 {
			continue
		}
		add(Finding{Code: "broker-without-replicas", Severity: SeverityWarning,
			Object: Object{Kind: ObjectBroker, ID: strconv.Itoa(int(b.ID))},
			Title:  "Broker without replicas",
			Explanation: fmt.Sprintf("Broker %d serves but holds no replicas, while the cluster has %d replicas. It takes no "+
				"load until replicas move to it, usually because it was added without a rebalance.", b.ID, total),
			Evidence:       []Evidence{{"replicas", "0"}, {"replicas in the cluster", strconv.Itoa(total)}, {"brokers", strconv.Itoa(len(brokers))}},
			Recommendation: "Rebalance the cluster to move replicas to the broker.",
			Fixes:          rebalance(in.View.Capabilities)})
	}
}

// balanced returns the brokers whose load the skew rules compare: serving, not cordoned, and
// only while no reassignment moves replicas.
func balanced(in *Input) []kafka.Broker {
	s := in.View.Snapshot
	if s == nil || len(s.Reassignments) > 0 || !in.View.Capabilities.Reassign {
		return nil
	}
	return placeable(s)
}

func replicaSkew(in *Input, add func(Finding)) {
	brokers := balanced(in)
	if len(brokers) < 2 {
		return
	}
	total := 0
	for _, b := range brokers {
		total += b.Replicas
	}
	mean := float64(total) / float64(len(brokers))
	var skewed []kafka.Broker
	for _, b := range brokers {
		if n := float64(b.Replicas); n > replicaSkewRatio*mean && n >= mean+replicaSkewMin {
			skewed = append(skewed, b)
		}
	}
	if len(skewed) == 0 {
		return
	}
	evidence := []Evidence{{"mean replicas per broker", fmt.Sprintf("%.0f", mean)}}
	for _, b := range skewed {
		evidence = append(evidence, Evidence{fmt.Sprintf("broker %d", b.ID), fmt.Sprintf("%d replicas", b.Replicas)})
	}
	add(Finding{Code: "replica-skew", Severity: SeverityInfo, Object: clusterObject(in),
		Title: "Replicas spread unevenly",
		Explanation: fmt.Sprintf("Brokers hold %.0f replicas on average, but %s hold more than %.1f times as many. Brokers "+
			"with more replicas serve more requests and leave more partitions under-replicated when they fail.",
			mean, brokerList(skewed), replicaSkewRatio),
		Evidence:       evidence,
		Recommendation: "Rebalance the cluster.",
		Fixes:          rebalance(in.View.Capabilities)})
}

func dataSkew(in *Input, add func(Finding)) {
	brokers := balanced(in)
	if len(brokers) < 2 || !in.View.Snapshot.Has(kafka.PartLogDirs) {
		return
	}
	var total int64
	for _, b := range brokers {
		total += b.DataBytes
	}
	mean := float64(total) / float64(len(brokers))
	var skewed []kafka.Broker
	for _, b := range brokers {
		if n := float64(b.DataBytes); n > dataSkewRatio*mean && b.DataBytes > dataSkewMin {
			skewed = append(skewed, b)
		}
	}
	if len(skewed) == 0 {
		return
	}
	evidence := []Evidence{{"mean data per broker", ops.HumanBytes(int64(mean))}}
	for _, b := range skewed {
		evidence = append(evidence, Evidence{fmt.Sprintf("broker %d", b.ID), ops.HumanBytes(b.DataBytes)})
	}
	add(Finding{Code: "data-skew", Severity: SeverityWarning, Object: clusterObject(in),
		Title: "Data spread unevenly",
		Explanation: fmt.Sprintf("Brokers hold %s of data on average, but %s hold more than %.1f times as much. Their disks "+
			"fill first, and replacing them copies more data.", ops.HumanBytes(int64(mean)), brokerList(skewed), dataSkewRatio),
		Evidence:       evidence,
		Recommendation: "Rebalance the cluster.",
		Fixes:          rebalance(in.View.Capabilities)})
}

func clusterObject(in *Input) Object {
	return Object{Kind: ObjectCluster, ID: in.View.Cluster.ID, Name: in.View.Cluster.Name}
}

// brokerList names brokers: "broker 101" or "brokers 101 and 102".
func brokerList(brokers []kafka.Broker) string {
	ids := make([]string, len(brokers))
	for i, b := range brokers {
		ids[i] = strconv.Itoa(int(b.ID))
	}
	if len(ids) == 1 {
		return "broker " + ids[0]
	}
	return "brokers " + strings.Join(ids[:len(ids)-1], ", ") + " and " + ids[len(ids)-1]
}

// agentsNotSeen finds the nodes of the cluster whose agent was not seen for more than a day.
func agentsNotSeen(in *Input, add func(Finding)) {
	for _, n := range in.Nodes {
		if n.ClusterID == nil || *n.ClusterID != in.View.Cluster.ID {
			continue
		}
		seen, ever := n.CreatedAt, n.LastSeenAt != nil
		if ever {
			seen = *n.LastSeenAt
		}
		silent := in.Now.Sub(seen)
		if silent <= agentSilence {
			continue
		}
		when := seen.UTC().Format(time.DateTime) + " UTC"
		state := fmt.Sprintf("was last seen %s ago, at %s", days(silent), when)
		lastSeen := when
		if !ever {
			state = fmt.Sprintf("has not connected since the node was created %s ago", days(silent))
			lastSeen = "never"
		}
		evidence := []Evidence{{"last seen", lastSeen}, {"host", n.Hostname}, {"role", n.Role}}
		if n.KafkaNodeID != nil {
			evidence = append(evidence, Evidence{"Kafka node", strconv.Itoa(int(*n.KafkaNodeID))})
		}
		add(Finding{Code: "agent-not-seen", Severity: SeverityWarning, Object: Object{Kind: ObjectNode, ID: n.ID, Name: n.Name},
			Title: "Agent not seen for more than a day",
			Explanation: fmt.Sprintf("The agent of node %s %s. Without it Brokerium cannot start, stop, restart, upgrade or "+
				"replace the Kafka node on this host.", n.Name, state),
			Evidence: evidence,
			Recommendation: "Check that the brokerium-agent service runs on the host and reaches the server. If the host is " +
				"gone, replace its Kafka node and delete the node in Brokerium."})
	}
}

// days renders a duration of at least a day as days and hours.
func days(d time.Duration) string {
	n := int(d / (24 * time.Hour))
	h := int(d%(24*time.Hour)) / int(time.Hour)
	out := fmt.Sprintf("%d day(s)", n)
	if h > 0 {
		out += fmt.Sprintf(" %d hour(s)", h)
	}
	return out
}

// healthGuide gives the health issues of a cluster a title and a recommendation.
var healthGuide = map[string]struct{ title, recommendation string }{
	"unreachable": {"Cluster unreachable",
		"Check the bootstrap servers, the network path from the server and the credentials of the cluster."},
	"brokers_unreachable": {"Brokers unreachable",
		"Start the brokers or check the network path from the server to their listeners."},
	"broker_fenced": {"Brokers not serving",
		"Check the logs of these brokers: a fenced broker lost its session with the controllers or is starting."},
	"broker_cordoned": {"Cordoned brokers", "Uncordon the brokers when their maintenance is over."},
	"disk_full": {"Broker disk almost full",
		"Free space now: lower the retention of large topics, add disks or brokers, or move partitions to brokers with free space."},
	"disk_high": {"Broker disk filling up",
		"Plan more space: lower the retention of large topics, add disks or brokers, or rebalance by disk."},
	"offline_partitions": {"Offline partitions",
		"Start the brokers that hold the replicas of these partitions; until then their records cannot be read or written."},
	"under_min_isr": {"Partitions below min.insync.replicas",
		"Bring back the brokers that stopped, or fix the replication of the affected topics; producers with acks=all fail meanwhile."},
	"under_replicated": {"Under-replicated partitions",
		"Check the brokers whose replicas fell behind: stopped brokers, full disks or overloaded networks."},
	"reassigning":      {"Reassignments in progress", "Wait until the reassignments finish before starting other changes."},
	"no_quorum_leader": {"No controller quorum leader", "Start the controllers so that a majority of the voters runs."},
	"quorum_at_risk": {"Controller quorum at risk",
		"Check the controllers that fall behind: one more failure can stop the quorum."},
	"voter_lagging":  {"Controller voter lagging", "Check the controller that falls behind: its disk, its network and its logs."},
	"even_voters":    {"Even number of controller voters", "Run an odd number of voters, for example 3 or 5."},
	"quorum_unknown": {"Controller quorum unknown", "Check that the Brokerium principal may describe the quorum."},
	"stale_observers": {"Nodes stopped fetching metadata",
		"Start these nodes again, or remove them: unregister stopped brokers and remove stopped controllers from the quorum."},
	"partial_snapshot": {"Incomplete snapshot",
		"Check the permissions of the Brokerium principal and the log of the server."},
	"agent_certificate_expired": {"Agent certificate expired",
		"Enroll the node again with a new join token."},
	"agent_certificate_expiring": {"Agent certificate not renewed",
		"Check that the agent is connected and at least version 0.2.0, so that it renews its certificate."},
}

// healthIssues turns every health issue of the cluster into a finding, one per issue code.
func healthIssues(in *Input, add func(Finding)) {
	var codes []string
	messages := map[string][]string{}
	severity := map[string]string{}
	for _, i := range in.View.Health.Issues {
		if _, ok := messages[i.Code]; !ok {
			codes = append(codes, i.Code)
		}
		messages[i.Code] = append(messages[i.Code], sentence(i.Message))
		if severity[i.Code] == "" || severityRank(i.Severity) < severityRank(severity[i.Code]) {
			severity[i.Code] = i.Severity
		}
	}
	for _, code := range codes {
		guide, ok := healthGuide[code]
		if !ok {
			guide.title = "Health issue " + code
			guide.recommendation = "See the health of the cluster on its Overview tab."
		}
		sev := severity[code]
		if sev != SeverityCritical && sev != SeverityWarning {
			sev = SeverityInfo
		}
		add(Finding{Code: "health-" + code, Severity: sev, Object: clusterObject(in), Title: guide.title,
			Explanation: strings.Join(messages[code], " "), Recommendation: guide.recommendation})
	}
}

// sentence starts a message with a capital letter and ends it with a period.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	s = string(unicode.ToUpper(r)) + s[size:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}
