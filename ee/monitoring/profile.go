// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"context"
	"strings"
	"time"
)

// Exporter generations: the metric names of their JVM metrics differ.
const (
	Exporter0 = "0.x"
	Exporter1 = "1.x"
)

// How long a detected profile is used.
const (
	profileTTL      = 5 * time.Minute
	profileErrorTTL = 30 * time.Second
)

// Metric names that tell the profile of a cluster apart.
const (
	heapMetric1    = "jvm_memory_used_bytes"
	heapMetric0    = "jvm_memory_bytes_used"
	raftMetric     = "kafka_server_raft_metrics_current_leader"
	partitionCount = "kafka_server_replicamanager_partitioncount"
)

// profileQuery counts the series of the metrics that tell the profile apart.
const profileQuery = `count by (__name__) ({__name__=~"` + heapMetric1 + `|` + heapMetric0 + `|` + raftMetric + `|` +
	partitionCount + `",$sel})`

// Profile is what the exporters of a cluster export.
type Profile struct {
	// Exporter is the generation of the JMX exporter; empty when no JVM metric matches.
	Exporter string `json:"exporter"`
	// HeapMetric is the JVM heap metric of that generation.
	HeapMetric string `json:"heapMetric"`
	// KRaft tells whether the metrics of the KRaft example configuration are present.
	KRaft bool `json:"kraft"`
	// Brokers is the number of brokers that export replica manager metrics.
	Brokers    int       `json:"brokers"`
	DetectedAt time.Time `json:"detectedAt"`
	Error      string    `json:"error,omitempty"`
}

// heapMetric is the JVM heap metric of the profile; unknown generations use the current one.
func (p Profile) heapMetric() string {
	if p.Exporter == Exporter0 {
		return heapMetric0
	}
	return heapMetric1
}

// idleMetric is the counter of the idle time of the request handlers: the exporter 1.x adds
// _total to counters.
func (p Profile) idleMetric() string {
	const name = "kafka_server_kafkarequesthandlerpool_requesthandleravgidlepercent_count"
	if p.Exporter == Exporter0 {
		return name
	}
	return name + "_total"
}

// detectProfile asks Prometheus which of the telling metrics the selector matches.
func detectProfile(ctx context.Context, q querier, selector string, now time.Time) Profile {
	p := Profile{DetectedAt: now}
	res, err := q.Query(ctx, strings.Replace(profileQuery, "$sel", selector, 1), now)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	for _, s := range res.Series {
		var n float64
		if len(s.Points) > 0 {
			n = s.Points[0].V
		}
		switch s.Labels["__name__"] {
		case heapMetric1:
			p.Exporter = Exporter1
		case heapMetric0:
			if p.Exporter == "" {
				p.Exporter = Exporter0
			}
		case raftMetric:
			p.KRaft = true
		case partitionCount:
			p.Brokers = int(n)
		}
	}
	if p.Exporter != "" {
		p.HeapMetric = p.heapMetric()
	}
	return p
}

type cachedProfile struct {
	profile Profile
	until   time.Time
}

// profileOf returns the profile of a cluster, detecting it when the cached one is too old.
func (svc *Service) profileOf(ctx context.Context, q querier, sourceFingerprint, clusterID, selector string) Profile {
	key := sourceFingerprint + "\x00" + clusterID + "\x00" + selector
	now := svc.now()
	svc.mu.Lock()
	c, ok := svc.profiles[key]
	svc.mu.Unlock()
	if ok && now.Before(c.until) {
		return c.profile
	}
	p := detectProfile(ctx, q, selector, now)
	ttl := profileTTL
	if p.Error != "" {
		ttl = profileErrorTTL
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	for k, old := range svc.profiles {
		if !now.Before(old.until) {
			delete(svc.profiles, k)
		}
	}
	svc.profiles[key] = cachedProfile{profile: p, until: now.Add(ttl)}
	return p
}
