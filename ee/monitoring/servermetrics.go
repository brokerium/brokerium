// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/brokerium/brokerium/ee/monitoring/exposition"
	"github.com/brokerium/brokerium/internal/api"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/store"
	"github.com/brokerium/brokerium/internal/version"
)

// Sources of the token of GET /metrics.
const (
	TokenFromEnvironment = "environment"
	TokenFromFile        = "file"
)

// metricsToken returns the bearer token of GET /metrics and where it comes from; an empty token
// turns the endpoint off. The file is read on every call, so a replaced Secret takes effect.
func (svc *Service) metricsToken() (token, source string, err error) {
	if svc.cfg.MetricsToken != "" {
		return svc.cfg.MetricsToken, TokenFromEnvironment, nil
	}
	if svc.cfg.MetricsTokenFile == "" {
		return "", "", nil
	}
	raw, err := os.ReadFile(svc.cfg.MetricsTokenFile) //nolint:gosec // the path comes from BROKERIUM_METRICS_TOKEN_FILE
	if err != nil {
		return "", TokenFromFile, fmt.Errorf("reading BROKERIUM_METRICS_TOKEN_FILE: %w", err)
	}
	return strings.TrimSpace(string(raw)), TokenFromFile, nil
}

type metricsEndpoint struct{ svc *Service }

// MetricsEndpoint returns the extension that serves GET /metrics.
func (svc *Service) MetricsEndpoint() api.Extension {
	return metricsEndpoint{svc: svc}
}

func (e metricsEndpoint) Register(s *api.Server) {
	s.Handle("GET /metrics", http.HandlerFunc(e.svc.serveMetrics))
}

// serveMetrics answers 404 without a token, 401 for a wrong one and 403 without a license that
// covers monitoring, in this order, so that only clients with the token learn about the license.
func (svc *Service) serveMetrics(w http.ResponseWriter, r *http.Request) {
	token, _, err := svc.metricsToken()
	switch {
	case err != nil:
		svc.log.Warn("GET /metrics", "error", err)
		api.WriteError(w, http.StatusServiceUnavailable, "metrics_unavailable", "the token of the metrics endpoint cannot be read; see the server log")
		return
	case token == "":
		api.WriteError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	got, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	want, have := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(got))
	if !bearer || subtle.ConstantTimeCompare(want[:], have[:]) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="brokerium"`)
		api.WriteError(w, http.StatusUnauthorized, "unauthenticated", "send the token of the metrics endpoint as a bearer token")
		return
	}
	if !edition.Allows(edition.Monitoring) {
		api.WriteError(w, http.StatusForbidden, "license_required", edition.LicenseRequired(edition.Monitoring).Error())
		return
	}
	w.Header().Set("Content-Type", exposition.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	if err := exposition.Write(w, svc.registry.Gather(r.Context())); err != nil {
		svc.log.Debug("writing the metrics", "error", err)
	}
}

// healthValue is the value of brokerium_cluster_health_status; unknown health has none.
func healthValue(status string) (float64, bool) {
	switch status {
	case cluster.HealthOK:
		return 0, true
	case cluster.HealthWarning:
		return 1, true
	case cluster.HealthCritical, cluster.HealthUnreachable:
		return 2, true
	}
	return 0, false
}

// collect returns the metrics of every cluster from the latest views, the estimates of the
// sampler and the active operations.
func (svc *Service) collect(ctx context.Context) []*exposition.Family {
	g := exposition.NewGauge
	build := g("brokerium_build_info", "Version of the Brokerium server; the value is 1.")
	build.Add(1, "version", version.Version, "commit", version.Commit, "edition", edition.Name)
	clusters, err := svc.store.ListClusters(ctx)
	if err != nil {
		svc.log.Warn("collecting the metrics of the clusters", "error", err)
		return []*exposition.Family{build}
	}
	active, err := svc.store.OperationsInStatus(ctx, store.OpPending, store.OpRunning, store.OpPaused)
	if err != nil {
		svc.log.Warn("collecting the active operations", "error", err)
	}
	opCounts := map[string]map[string]int{}
	for _, op := range active {
		if opCounts[op.ClusterID] == nil {
			opCounts[op.ClusterID] = map[string]int{}
		}
		opCounts[op.ClusterID][op.Status]++
	}

	var (
		info         = g("brokerium_cluster_info", "A registered cluster; the value is 1.")
		healthStatus = g("brokerium_cluster_health_status", "Health of the cluster: 0 ok, 1 warning, 2 critical or unreachable.")
		reachable    = g("brokerium_cluster_reachable", "Whether the brokers of the cluster answered the last snapshot.")
		refreshed    = g("brokerium_cluster_last_snapshot_timestamp_seconds", "When the server last evaluated the cluster, in Unix seconds.")
		operations   = g("brokerium_cluster_operations", "Operations of the cluster that are pending, running or paused.")
		brokers      = g("brokerium_cluster_brokers", "Brokers registered in the cluster.")
		alive        = g("brokerium_cluster_brokers_alive", "Brokers that serve requests.")
		fenced       = g("brokerium_cluster_brokers_fenced", "Brokers that are registered but fenced.")
		topics       = g("brokerium_cluster_topics", "Topics of the cluster.")
		partitions   = g("brokerium_cluster_partitions", "Partitions of the cluster.")
		underRepl    = g("brokerium_cluster_partitions_under_replicated", "Partitions with fewer in-sync replicas than replicas, without those being reassigned.")
		underMinISR  = g("brokerium_cluster_partitions_under_min_isr", "Partitions with fewer in-sync replicas than min.insync.replicas; acks=all writes fail.")
		offline      = g("brokerium_cluster_partitions_offline", "Partitions without a leader.")
		voters       = g("brokerium_cluster_quorum_voters", "Voters of the KRaft controller quorum.")
		votersLag    = g("brokerium_cluster_quorum_voters_lagging", "Voters of the controller quorum that lag behind the leader.")
		diskUsed     = g("brokerium_broker_disk_used_bytes", "Used bytes of the volumes of the log directories of a broker.")
		diskTotal    = g("brokerium_broker_disk_total_bytes", "Size of the volumes of the log directories of a broker.")
		logSize      = g("brokerium_broker_log_size_bytes", "Bytes of the partition replicas of a broker.")
		brokerBytes  = g("brokerium_broker_estimated_bytes_in_per_second", "Bytes written to the partition replicas of a broker, "+
			"estimated from log sizes over five minutes.")
		brokerMsgs = g("brokerium_broker_estimated_messages_in_per_second", "Messages written to the partition replicas of a broker, "+
			"estimated from offsets over five minutes.")
		topicBytes = g("brokerium_topic_estimated_bytes_in_per_second", "Bytes written to a topic, estimated from log sizes over five minutes.")
		topicMsgs  = g("brokerium_topic_estimated_messages_in_per_second", "Messages written to a topic, estimated from offsets over five minutes.")
		groupLag   = g("brokerium_consumer_group_lag", "Messages a consumer or share group has not consumed yet, per topic.")
		groupMem   = g("brokerium_consumer_group_members", "Members of a consumer or share group.")
	)
	for _, c := range clusters {
		name := c.Name
		info.Add(1, "cluster", name, "provider", c.Provider, "flavor", c.Flavor, "kafka_cluster_id", c.KafkaClusterID)
		for _, status := range []string{store.OpPending, store.OpRunning, store.OpPaused} {
			operations.Add(float64(opCounts[c.ID][status]), "cluster", name, "status", status)
		}
		cached, ok := svc.views.Load(c.ID)
		if !ok {
			continue
		}
		v := cached.(*cluster.View)
		if value, ok := healthValue(v.Health.Status); ok {
			healthStatus.Add(value, "cluster", name)
		}
		reachable.Add(boolValue(v.Error == ""), "cluster", name)
		refreshed.Add(float64(v.UpdatedAt.UnixMilli())/1000, "cluster", name)
		if v.Error != "" || v.Snapshot == nil {
			continue
		}
		h, snap := v.Health, v.Snapshot
		brokers.Add(float64(h.Brokers), "cluster", name)
		alive.Add(float64(h.BrokersAlive), "cluster", name)
		fenced.Add(float64(h.BrokersFenced), "cluster", name)
		topics.Add(float64(h.Topics), "cluster", name)
		partitions.Add(float64(h.Partitions), "cluster", name)
		underRepl.Add(float64(h.UnderReplicated), "cluster", name)
		underMinISR.Add(float64(h.UnderMinISR), "cluster", name)
		offline.Add(float64(h.OfflinePartitions), "cluster", name)
		if snap.Quorum != nil {
			voters.Add(float64(h.Voters), "cluster", name)
			votersLag.Add(float64(h.VotersLagging), "cluster", name)
		}
		for _, b := range snap.Brokers {
			id := strconv.Itoa(int(b.ID))
			if b.DiskTotalBytes > 0 {
				diskUsed.Add(float64(b.DiskTotalBytes-b.DiskUsableBytes), "cluster", name, "broker", id)
				diskTotal.Add(float64(b.DiskTotalBytes), "cluster", name, "broker", id)
			}
			if len(b.LogDirs) > 0 {
				logSize.Add(float64(b.DataBytes), "cluster", name, "broker", id)
			}
		}
		for _, grp := range snap.Groups {
			if grp.Error != "" {
				continue
			}
			groupMem.Add(float64(grp.Members), "cluster", name, "group", grp.ID)
			for _, topic := range sortedKeys(grp.TopicLag) {
				groupLag.Add(float64(grp.TopicLag[topic]), "cluster", name, "group", grp.ID, "topic", topic)
			}
		}
		if svc.sampler == nil {
			continue
		}
		if rates := svc.sampler.Rates(c.ID); rates != nil {
			ids := sortedKeys(rates.Brokers)
			slices.SortFunc(ids, func(a, b string) int { return compareNames(a, b) })
			for _, id := range ids {
				brokerBytes.Add(rates.Brokers[id].BytesInPerSec, "cluster", name, "broker", id)
				brokerMsgs.Add(rates.Brokers[id].MessagesInPerSec, "cluster", name, "broker", id)
			}
			for _, topic := range sortedKeys(rates.Topics) {
				topicBytes.Add(rates.Topics[topic].BytesInPerSec, "cluster", name, "topic", topic)
				topicMsgs.Add(rates.Topics[topic].MessagesInPerSec, "cluster", name, "topic", topic)
			}
		}
	}
	return []*exposition.Family{build, info, healthStatus, reachable, refreshed, operations, brokers, alive, fenced, topics,
		partitions, underRepl, underMinISR, offline, voters, votersLag, diskUsed, diskTotal, logSize, brokerBytes, brokerMsgs,
		topicBytes, topicMsgs, groupLag, groupMem}
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
