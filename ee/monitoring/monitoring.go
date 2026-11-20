// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

// Package monitoring is the monitoring feature of Brokerium Enterprise (ADR 0024): charts of the
// metrics that the team's Prometheus collects from the JMX exporters of the brokers, and
// GET /metrics with what only Brokerium knows about the clusters.
package monitoring

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/brokerium/brokerium/ee/monitoring/exposition"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/config"
	"github.com/brokerium/brokerium/internal/metrics"
	"github.com/brokerium/brokerium/internal/store"
)

// Store is the part of the store that the monitoring feature uses.
type Store interface {
	GetSetting(ctx context.Context, key string) (*store.Setting, error)
	SetSetting(ctx context.Context, st *store.Setting) error
	DeleteSetting(ctx context.Context, key string) error
	GetClusterMonitoring(ctx context.Context, clusterID string) (*store.ClusterMonitoring, error)
	SetClusterMonitoring(ctx context.Context, m *store.ClusterMonitoring) error
	ListClusters(ctx context.Context) ([]*store.Cluster, error)
	OperationsInStatus(ctx context.Context, statuses ...string) ([]*store.Operation, error)
}

// Clusters gives the views of clusters; *cluster.Service is one.
type Clusters interface {
	View(ctx context.Context, id string, maxAge time.Duration) (*cluster.View, error)
	OnRefresh(fn func(*cluster.View))
}

// Deps are what the monitoring feature builds on.
type Deps struct {
	Config   config.Server
	Store    Store
	Box      Sealer
	Clusters Clusters
	Sampler  *metrics.Sampler
	Log      *slog.Logger
	// Registry receives the metrics of the clusters; nil means exposition.Default.
	Registry *exposition.Registry
}

// Service serves the monitoring feature.
type Service struct {
	cfg      config.Server
	store    Store
	box      Sealer
	clusters Clusters
	sampler  *metrics.Sampler
	log      *slog.Logger
	registry *exposition.Registry
	now      func() time.Time

	mu       sync.Mutex
	client   *Client
	clientFP string
	profiles map[string]cachedProfile

	// views are the latest views of the clusters, for GET /metrics without waiting for Kafka.
	views sync.Map
}

// New checks the settings of the environment and returns the service; it registers the metrics
// of the clusters with the registry of GET /metrics.
func New(d Deps) (*Service, error) {
	if err := checkEnvironment(d.Config.Prometheus); err != nil {
		return nil, err
	}
	svc := &Service{cfg: d.Config, store: d.Store, box: d.Box, clusters: d.Clusters, sampler: d.Sampler, log: d.Log,
		registry: d.Registry, now: time.Now, profiles: map[string]cachedProfile{}}
	if svc.registry == nil {
		svc.registry = exposition.Default
	}
	if d.Config.Prometheus.URL != "" {
		if _, err := environmentSource(d.Config.Prometheus); err != nil {
			d.Log.Warn("the Prometheus data source cannot be used yet", "error", err)
		}
		d.Log.Info("monitoring uses the Prometheus of BROKERIUM_PROMETHEUS_URL", "url", d.Config.Prometheus.URL)
	}
	if _, source, err := svc.metricsToken(); err != nil {
		d.Log.Warn("GET /metrics answers 503 until its token can be read", "error", err)
	} else if source != "" {
		d.Log.Info("GET /metrics is on", "token", source)
	}
	if d.Clusters != nil {
		d.Clusters.OnRefresh(func(v *cluster.View) {
			if v != nil && v.Cluster != nil {
				svc.views.Store(v.Cluster.ID, v)
			}
		})
	}
	svc.registry.Register("clusters", svc.collect)
	return svc, nil
}

// ErrNotConfigured is returned for queries without a data source.
var ErrNotConfigured = errors.New("no Prometheus data source is configured; an administrator adds one on the Monitoring page")

// clientFor returns the source in use and a client for it; the client is kept while the source
// stays the same.
func (svc *Service) clientFor(ctx context.Context) (*Source, *Client, error) {
	src, err := svc.loadSource(ctx)
	if err != nil {
		return src, nil, err
	}
	if src == nil {
		return nil, nil, ErrNotConfigured
	}
	fp := src.fingerprint()
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.client != nil && svc.clientFP == fp {
		return src, svc.client, nil
	}
	c, err := NewClient(src)
	if err != nil {
		return src, nil, fmt.Errorf("the data source cannot be used: %w", err)
	}
	if svc.client != nil {
		svc.client.Close()
	}
	svc.client, svc.clientFP = c, fp
	return src, c, nil
}

// forgetSource drops the cached client and profiles after the source changed.
func (svc *Service) forgetSource() {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.client != nil {
		svc.client.Close()
	}
	svc.client, svc.clientFP = nil, ""
	svc.profiles = map[string]cachedProfile{}
}

// SelectorView is how a cluster is found in Prometheus.
type SelectorView struct {
	// Effective is the selector the panels use: Custom, else Default.
	Effective    string     `json:"effective"`
	Custom       string     `json:"custom"`
	Default      string     `json:"default"`
	ExporterPort int        `json:"exporterPort"`
	UpdatedAt    *time.Time `json:"updatedAt,omitempty"`
	UpdatedBy    string     `json:"updatedBy,omitempty"`
}

// targetOf returns the selector of a cluster and the names of its nodes.
func (svc *Service) targetOf(ctx context.Context, clusterID string) (target, SelectorView, error) {
	v, err := svc.clusters.View(ctx, clusterID, time.Minute)
	if err != nil {
		return target{}, SelectorView{}, err
	}
	sel := SelectorView{ExporterPort: DefaultExporterPort}
	m, err := svc.store.GetClusterMonitoring(ctx, clusterID)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return target{}, sel, fmt.Errorf("reading the monitoring settings of the cluster: %w", err)
	default:
		sel.Custom = m.Selector
		if m.ExporterPort > 0 {
			sel.ExporterPort = m.ExporterPort
		}
		at := m.UpdatedAt
		sel.UpdatedAt, sel.UpdatedBy = &at, m.UpdatedBy
	}
	n := clusterNodes(v)
	sel.Default = defaultSelector(n.hosts, sel.ExporterPort)
	sel.Effective = sel.Default
	if sel.Custom != "" {
		sel.Effective = sel.Custom
	}
	return target{selector: sel.Effective, nodes: n}, sel, nil
}
