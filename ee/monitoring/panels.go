// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

// Sections of the Monitoring tab.
const (
	SectionCluster = "cluster"
	SectionBroker  = "broker"
	SectionTopic   = "topic"
)

// Units of panels; the dashboard formats values by them.
const (
	UnitBytes             = "bytes"
	UnitBytesPerSecond    = "bytesPerSecond"
	UnitMessagesPerSecond = "messagesPerSecond"
	UnitRequestsPerSecond = "requestsPerSecond"
	UnitPerMinute         = "perMinute"
	UnitMilliseconds      = "milliseconds"
	UnitPercent           = "percent"
	UnitCores             = "cores"
	UnitCount             = "count"
)

// Query is a PromQL expression of a panel. It uses these placeholders:
//
//	$sel     the label matchers of the cluster, such as instance=~"kafka-1:9141|kafka-2:9141"
//	$rate    the window of rate(), at least one step and five minutes
//	$range   the window that ranks the topics of top-N panels: the range, up to six hours
//	$topics  a regular expression of the topics that the ranking found
//	$heap    the JVM heap metric of the exporter generation
//	$idle    the counter of the request handler idle time of the exporter generation
type Query struct {
	Expr string
	// Legend names the series of a query that returns one series.
	Legend string
}

// Panel is a chart of the Monitoring tab: fixed queries that the server runs, so that clients
// choose panels and never send PromQL.
type Panel struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Section     string `json:"section"`
	Unit        string `json:"unit"`
	Description string `json:"description,omitempty"`

	queries []Query
	// by is the label that names the series of queries that group by it.
	by string
	// rank is an instant query that selects the series of a top-N panel; it returns the values
	// of the label by.
	rank string
	// kraft panels need the metrics of the KRaft example configuration of the exporter.
	kraft bool
}

const (
	bytesIn     = `kafka_server_brokertopicmetrics_bytesin_total`
	bytesOut    = `kafka_server_brokertopicmetrics_bytesout_total`
	messagesIn  = `kafka_server_brokertopicmetrics_messagesin_total`
	requests    = `kafka_network_requestmetrics_requests_total`
	requestTime = `kafka_network_requestmetrics_totaltimems`
	clientReqs  = `request=~"Produce|FetchConsumer|FetchFollower"`
)

// Panels lists the panels in the order of the tab. The metric names are those of the example
// configurations kafka-2_0_0.yml and kafka-kraft-3_0_0.yml of the Prometheus JMX exporter.
var Panels = []Panel{
	{ID: "cluster-bytes", Title: "Bytes in and out", Section: SectionCluster, Unit: UnitBytesPerSecond,
		Description: "Bytes that producers write and that consumers read, without replication.",
		queries: []Query{
			{Legend: "in", Expr: `sum(rate(` + bytesIn + `{$sel,topic=""}[$rate]))`},
			{Legend: "out", Expr: `sum(rate(` + bytesOut + `{$sel,topic=""}[$rate]))`},
		}},
	{ID: "cluster-messages", Title: "Messages in", Section: SectionCluster, Unit: UnitMessagesPerSecond,
		queries: []Query{{Legend: "messages", Expr: `sum(rate(` + messagesIn + `{$sel,topic=""}[$rate]))`}}},
	{ID: "cluster-requests", Title: "Produce and fetch requests", Section: SectionCluster, Unit: UnitRequestsPerSecond, by: "request",
		queries: []Query{{Expr: `sum by (request) (rate(` + requests + `{$sel,` + clientReqs + `}[$rate]))`}}},
	{ID: "cluster-request-time", Title: "Request time, 99th percentile", Section: SectionCluster, Unit: UnitMilliseconds, by: "request",
		Description: "Total time of the requests on the slowest broker.",
		queries:     []Query{{Expr: `max by (request) (` + requestTime + `{$sel,` + clientReqs + `,quantile="0.99"})`}}},
	{ID: "cluster-partitions", Title: "Partitions at risk", Section: SectionCluster, Unit: UnitCount,
		Description: "Under-replicated partitions, partitions under min.insync.replicas and partitions without a leader.",
		queries: []Query{
			{Legend: "under-replicated", Expr: `sum(kafka_server_replicamanager_underreplicatedpartitions{$sel})`},
			{Legend: "under min ISR", Expr: `sum(kafka_server_replicamanager_underminisrpartitioncount{$sel})`},
			{Legend: "offline", Expr: `max(kafka_controller_kafkacontroller_offlinepartitionscount{$sel})`},
		}},
	{ID: "cluster-controllers", Title: "Active controllers", Section: SectionCluster, Unit: UnitCount,
		Description: "Exactly one node is the active controller.",
		queries:     []Query{{Legend: "active controllers", Expr: `sum(kafka_controller_kafkacontroller_activecontrollercount{$sel})`}}},
	{ID: "cluster-isr", Title: "ISR shrinks and expands", Section: SectionCluster, Unit: UnitPerMinute,
		queries: []Query{
			{Legend: "shrinks", Expr: `60 * sum(rate(kafka_server_replicamanager_isrshrinks_total{$sel}[$rate]))`},
			{Legend: "expands", Expr: `60 * sum(rate(kafka_server_replicamanager_isrexpands_total{$sel}[$rate]))`},
		}},
	{ID: "cluster-quorum", Title: "Metadata quorum epoch", Section: SectionCluster, Unit: UnitCount, kraft: true,
		Description: "Rises with every election of a quorum leader.",
		queries:     []Query{{Legend: "epoch", Expr: `max(kafka_server_raft_metrics_current_epoch{$sel})`}}},

	{ID: "broker-bytes-in", Title: "Bytes in", Section: SectionBroker, Unit: UnitBytesPerSecond, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (rate(` + bytesIn + `{$sel,topic=""}[$rate]))`}}},
	{ID: "broker-bytes-out", Title: "Bytes out", Section: SectionBroker, Unit: UnitBytesPerSecond, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (rate(` + bytesOut + `{$sel,topic=""}[$rate]))`}}},
	{ID: "broker-messages", Title: "Messages in", Section: SectionBroker, Unit: UnitMessagesPerSecond, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (rate(` + messagesIn + `{$sel,topic=""}[$rate]))`}}},
	{ID: "broker-produce-requests", Title: "Produce requests", Section: SectionBroker, Unit: UnitRequestsPerSecond, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (rate(` + requests + `{$sel,request="Produce"}[$rate]))`}}},
	{ID: "broker-fetch-requests", Title: "Consumer fetch requests", Section: SectionBroker, Unit: UnitRequestsPerSecond, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (rate(` + requests + `{$sel,request="FetchConsumer"}[$rate]))`}}},
	{ID: "broker-produce-time", Title: "Produce time, 99th percentile", Section: SectionBroker, Unit: UnitMilliseconds, by: "instance",
		queries: []Query{{Expr: `max by (instance) (` + requestTime + `{$sel,request="Produce",quantile="0.99"})`}}},
	{ID: "broker-fetch-time", Title: "Consumer fetch time, 99th percentile", Section: SectionBroker, Unit: UnitMilliseconds, by: "instance",
		queries: []Query{{Expr: `max by (instance) (` + requestTime + `{$sel,request="FetchConsumer",quantile="0.99"})`}}},
	{ID: "broker-under-replicated", Title: "Under-replicated partitions", Section: SectionBroker, Unit: UnitCount, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (kafka_server_replicamanager_underreplicatedpartitions{$sel})`}}},
	{ID: "broker-under-min-isr", Title: "Partitions under min ISR", Section: SectionBroker, Unit: UnitCount, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (kafka_server_replicamanager_underminisrpartitioncount{$sel})`}}},
	{ID: "broker-isr-shrinks", Title: "ISR shrinks", Section: SectionBroker, Unit: UnitPerMinute, by: "instance",
		queries: []Query{{Expr: `60 * sum by (instance) (rate(kafka_server_replicamanager_isrshrinks_total{$sel}[$rate]))`}}},
	{ID: "broker-partitions", Title: "Partitions", Section: SectionBroker, Unit: UnitCount, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (kafka_server_replicamanager_partitioncount{$sel})`}}},
	{ID: "broker-leaders", Title: "Leaders", Section: SectionBroker, Unit: UnitCount, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (kafka_server_replicamanager_leadercount{$sel})`}}},
	{ID: "broker-request-handler-idle", Title: "Request handler idle", Section: SectionBroker, Unit: UnitPercent, by: "instance",
		Description: "Share of time the request handler threads wait for requests; low values mean the broker is saturated.",
		queries:     []Query{{Expr: `100 * sum by (instance) (rate($idle{$sel}[$rate])) / 1e9`}}},
	{ID: "broker-network-idle", Title: "Network processor idle", Section: SectionBroker, Unit: UnitPercent, by: "instance",
		queries: []Query{{Expr: `100 * avg by (instance) (kafka_network_socketserver_networkprocessoravgidlepercent{$sel})`}}},
	{ID: "broker-heap", Title: "JVM heap used", Section: SectionBroker, Unit: UnitBytes, by: "instance",
		queries: []Query{{Expr: `sum by (instance) ($heap{$sel,area="heap"})`}}},
	{ID: "broker-gc", Title: "Time in garbage collection", Section: SectionBroker, Unit: UnitPercent, by: "instance",
		queries: []Query{{Expr: `100 * sum by (instance) (rate(jvm_gc_collection_seconds_sum{$sel}[$rate]))`}}},
	{ID: "broker-cpu", Title: "CPU", Section: SectionBroker, Unit: UnitCores, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (rate(process_cpu_seconds_total{$sel}[$rate]))`}}},
	{ID: "broker-open-files", Title: "Open files", Section: SectionBroker, Unit: UnitCount, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (process_open_fds{$sel})`}}},
	{ID: "broker-log-size", Title: "Log size", Section: SectionBroker, Unit: UnitBytes, by: "instance",
		queries: []Query{{Expr: `sum by (instance) (kafka_log_log_size{$sel})`}}},

	{ID: "topic-bytes-in", Title: "Top topics by bytes in", Section: SectionTopic, Unit: UnitBytesPerSecond, by: "topic",
		rank:    `topk(10, sum by (topic) (rate(` + bytesIn + `{$sel,topic!=""}[$range])))`,
		queries: []Query{{Expr: `sum by (topic) (rate(` + bytesIn + `{$sel,topic=~"$topics"}[$rate]))`}}},
	{ID: "topic-bytes-out", Title: "Top topics by bytes out", Section: SectionTopic, Unit: UnitBytesPerSecond, by: "topic",
		rank:    `topk(10, sum by (topic) (rate(` + bytesOut + `{$sel,topic!=""}[$range])))`,
		queries: []Query{{Expr: `sum by (topic) (rate(` + bytesOut + `{$sel,topic=~"$topics"}[$rate]))`}}},
	{ID: "topic-messages", Title: "Top topics by messages in", Section: SectionTopic, Unit: UnitMessagesPerSecond, by: "topic",
		rank:    `topk(10, sum by (topic) (rate(` + messagesIn + `{$sel,topic!=""}[$range])))`,
		queries: []Query{{Expr: `sum by (topic) (rate(` + messagesIn + `{$sel,topic=~"$topics"}[$rate]))`}}},
	{ID: "topic-log-size", Title: "Top topics by size", Section: SectionTopic, Unit: UnitBytes, by: "topic",
		Description: "Log size of all replicas.",
		rank:        `topk(10, sum by (topic) (kafka_log_log_size{$sel}))`,
		queries:     []Query{{Expr: `sum by (topic) (kafka_log_log_size{$sel,topic=~"$topics"})`}}},
}

// lookupPanel returns the panel with an id.
func lookupPanel(id string) (Panel, bool) {
	for _, p := range Panels {
		if p.ID == id {
			return p, true
		}
	}
	return Panel{}, false
}

// panelsFor lists the panels that a profile can show.
func panelsFor(p *Profile) []Panel {
	out := make([]Panel, 0, len(Panels))
	for _, panel := range Panels {
		if panel.kraft && (p == nil || !p.KRaft) {
			continue
		}
		out = append(out, panel)
	}
	return out
}
