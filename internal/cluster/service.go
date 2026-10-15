// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package cluster keeps the registry of clusters, their Kafka clients, cached snapshots,
// health and capabilities.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bvaliev/brokerium/internal/kafka"
	"github.com/bvaliev/brokerium/internal/secrets"
	"github.com/bvaliev/brokerium/internal/store"
)

// View is everything the dashboard shows about one cluster.
type View struct {
	Cluster      *store.Cluster  `json:"cluster"`
	Snapshot     *kafka.Snapshot `json:"snapshot,omitempty"`
	Health       Health          `json:"health"`
	Capabilities Capabilities    `json:"capabilities"`
	Error        string          `json:"error,omitempty"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

type cachedClient struct {
	client    *kafka.Client
	updatedAt time.Time
}

// Service manages clients and snapshots of registered clusters.
type Service struct {
	store *store.Store
	box   *secrets.Box
	log   *slog.Logger

	mu        sync.Mutex
	clients   map[string]cachedClient
	views     map[string]*View
	refreshMu map[string]*sync.Mutex
	listeners []func(*View)
}

// New creates a Service.
func New(st *store.Store, box *secrets.Box, log *slog.Logger) *Service {
	return &Service{store: st, box: box, log: log, clients: map[string]cachedClient{}, views: map[string]*View{},
		refreshMu: map[string]*sync.Mutex{}}
}

// OnRefresh registers a callback invoked after each snapshot refresh.
func (s *Service) OnRefresh(fn func(*View)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

// Profile decrypts the credentials of a cluster.
func (s *Service) Profile(c *store.Cluster) (kafka.Profile, error) {
	p := kafka.Profile{BootstrapServers: c.BootstrapServers, Security: c.Security}
	if err := s.box.OpenJSON(c.SecretsSealed, &p.Secrets); err != nil {
		return p, fmt.Errorf("decrypting credentials: %w", err)
	}
	return p, nil
}

// Client returns a cached Kafka client for the cluster.
func (s *Service) Client(ctx context.Context, id string) (*kafka.Client, *store.Cluster, error) {
	c, err := s.store.GetCluster(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	cached, ok := s.clients[id]
	s.mu.Unlock()
	if ok && cached.updatedAt.Equal(c.UpdatedAt) {
		return cached.client, c, nil
	}
	p, err := s.Profile(c)
	if err != nil {
		return nil, nil, err
	}
	cl, err := kafka.NewClient(p)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	if old, ok := s.clients[id]; ok {
		old.client.Close()
	}
	s.clients[id] = cachedClient{client: cl, updatedAt: c.UpdatedAt}
	s.mu.Unlock()
	return cl, c, nil
}

// Invalidate drops the cached client and view of a cluster.
func (s *Service) Invalidate(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.clients[id]; ok {
		cached.client.Close()
		delete(s.clients, id)
	}
	delete(s.views, id)
}

// Refresh collects a new snapshot now.
func (s *Service) Refresh(ctx context.Context, id string) (*View, error) {
	s.mu.Lock()
	lock, ok := s.refreshMu[id]
	if !ok {
		lock = &sync.Mutex{}
		s.refreshMu[id] = lock
	}
	s.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()

	cl, c, err := s.Client(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		v := &View{Cluster: c, Health: Health{Status: HealthUnreachable, Issues: []Issue{}}, Error: err.Error(), UpdatedAt: time.Now()}
		return v, nil
	}
	snapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	snap, err := cl.Snapshot(snapCtx, kafka.SnapshotOptions{Groups: true, MaxGroups: 500})
	v := &View{Cluster: c, UpdatedAt: time.Now()}
	if err != nil {
		v.Health = Health{Status: HealthUnreachable, Issues: []Issue{{Severity: HealthCritical, Code: "unreachable", Message: err.Error()}}}
		v.Error = err.Error()
		v.Capabilities = CapabilitiesFor(c, false)
	} else {
		v.Snapshot = snap
		v.Health = Evaluate(snap, time.Now())
		v.Capabilities = CapabilitiesFor(c, v.Health.KRaftVersion == 0 && snap.Quorum != nil)
		if snap.ClusterID != "" && snap.ClusterID != c.KafkaClusterID {
			if err := s.store.SetKafkaClusterID(ctx, c.ID, snap.ClusterID); err == nil {
				c.KafkaClusterID = snap.ClusterID
			}
		}
	}
	s.mu.Lock()
	s.views[id] = v
	listeners := append([]func(*View){}, s.listeners...)
	s.mu.Unlock()
	for _, fn := range listeners {
		fn(v)
	}
	return v, nil
}

// View returns the cached view, refreshing it when older than maxAge.
func (s *Service) View(ctx context.Context, id string, maxAge time.Duration) (*View, error) {
	s.mu.Lock()
	v, ok := s.views[id]
	s.mu.Unlock()
	if ok && time.Since(v.UpdatedAt) <= maxAge {
		c, err := s.store.GetCluster(ctx, id)
		if err != nil {
			return nil, err
		}
		cp := *v
		cp.Cluster = c
		return &cp, nil
	}
	return s.Refresh(ctx, id)
}

// Views returns views of all clusters, refreshing missing ones.
func (s *Service) Views(ctx context.Context, maxAge time.Duration) ([]*View, error) {
	clusters, err := s.store.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*View, len(clusters))
	var wg sync.WaitGroup
	for i, c := range clusters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.View(ctx, c.ID, maxAge)
			if err != nil {
				v = &View{Cluster: c, Health: Health{Status: HealthUnknown, Issues: []Issue{}}, Error: err.Error()}
			}
			out[i] = v
		}()
	}
	wg.Wait()
	return out, nil
}

// Run refreshes all clusters periodically until ctx ends.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		clusters, err := s.store.ListClusters(ctx)
		if err == nil {
			var wg sync.WaitGroup
			for _, c := range clusters {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, err := s.Refresh(ctx, c.ID); err != nil && ctx.Err() == nil {
						s.log.Warn("refreshing cluster", "cluster", c.Name, "error", err)
					}
				}()
			}
			wg.Wait()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// TestResult reports a connection test.
type TestResult struct {
	OK        bool   `json:"ok"`
	ClusterID string `json:"clusterId,omitempty"`
	Brokers   int    `json:"brokers"`
	Error     string `json:"error,omitempty"`
}

// TestConnection connects with a profile and reads metadata.
func TestConnection(ctx context.Context, p kafka.Profile) TestResult {
	cl, err := kafka.NewClient(p)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	md, err := cl.Adm.BrokerMetadata(ctx)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	return TestResult{OK: true, ClusterID: md.Cluster, Brokers: len(md.Brokers)}
}
