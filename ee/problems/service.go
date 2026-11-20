// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package problems

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/store"
)

const (
	// seenEvery is how often the last-seen time of a finding is written at most.
	seenEvery = time.Minute
	// forgetAfter is how long the state of a finding stays after it was last seen.
	forgetAfter = 30 * 24 * time.Hour
	// forgetEvery is how often states older than forgetAfter are deleted.
	forgetEvery = time.Hour
	// viewAge is how old the view of a cluster may be when the API reads its problems.
	viewAge = 20 * time.Second
)

// errNotCurrent is returned for a fingerprint that no current finding of the cluster has.
var errNotCurrent = errors.New("the cluster has no current problem with this fingerprint")

// stateStore is the part of the store that the service uses.
type stateStore interface {
	ListClusters(ctx context.Context) ([]*store.Cluster, error)
	ListNodes(ctx context.Context, clusterID string) ([]*store.Node, error)
	SeeProblems(ctx context.Context, clusterID string, seen []store.ProblemKey, at time.Time) error
	ProblemStates(ctx context.Context, clusterID string) ([]*store.ProblemState, error)
	AcknowledgeProblem(ctx context.Context, clusterID string, k store.ProblemKey, actor string, at time.Time) (*store.ProblemState, error)
	UnacknowledgeProblem(ctx context.Context, clusterID, fingerprint string) (*store.ProblemState, error)
	MuteProblem(ctx context.Context, clusterID string, k store.ProblemKey, until, now time.Time) (*store.ProblemState, error)
	UnmuteProblem(ctx context.Context, clusterID, fingerprint string) (*store.ProblemState, error)
	ForgetProblems(ctx context.Context, before time.Time) (int64, error)
}

// clusterViews is the part of the cluster service that the service uses.
type clusterViews interface {
	OnRefresh(fn func(*cluster.View))
	View(ctx context.Context, id string, maxAge time.Duration) (*cluster.View, error)
	Views(ctx context.Context, maxAge time.Duration) ([]*cluster.View, error)
}

// Service evaluates the rules after every snapshot of a cluster, keeps the findings in memory
// and records when they were seen.
type Service struct {
	store    stateStore
	clusters clusterViews
	log      *slog.Logger
	now      func() time.Time

	mu      sync.Mutex
	latest  map[string]evaluation
	pending map[string]*cluster.View
	// written holds, by cluster and fingerprint, when the last-seen time was written.
	written map[string]map[string]time.Time
	wake    chan struct{}
}

// evaluation is the result of the rules for the view of a cluster updated at viewAt.
type evaluation struct {
	viewAt   time.Time
	findings []Finding
}

// New creates the service; Start subscribes it to the snapshots.
func New(st *store.Store, clusters *cluster.Service, log *slog.Logger) *Service {
	return newService(st, clusters, log)
}

func newService(st stateStore, clusters clusterViews, log *slog.Logger) *Service {
	return &Service{store: st, clusters: clusters, log: log, now: time.Now, latest: map[string]evaluation{},
		pending: map[string]*cluster.View{}, written: map[string]map[string]time.Time{}, wake: make(chan struct{}, 1)}
}

// Start subscribes to the refreshes of the clusters and records their findings in the
// background until ctx ends.
func (s *Service) Start(ctx context.Context) {
	s.clusters.OnRefresh(s.observe)
	go s.run(ctx)
}

// observe queues a refreshed view; refreshes run in the operations and the API, so the rules
// and the database writes wait for the background loop.
func (s *Service) observe(v *cluster.View) {
	if v == nil || v.Cluster == nil {
		return
	}
	s.mu.Lock()
	s.pending[v.Cluster.ID] = v
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) run(ctx context.Context) {
	t := time.NewTicker(forgetEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			s.recordPending(ctx)
		case <-t.C:
			s.forget(ctx)
		}
	}
}

func (s *Service) recordPending(ctx context.Context) {
	s.mu.Lock()
	views := s.pending
	s.pending = map[string]*cluster.View{}
	s.mu.Unlock()
	for id, v := range views {
		findings, err := s.findings(ctx, v)
		if err == nil {
			err = s.record(ctx, id, findings)
		}
		if err != nil && ctx.Err() == nil {
			s.log.Warn("recording problems", "cluster", v.Cluster.Name, "error", err)
		}
	}
}

// findings returns the findings of a view; the rules run once per snapshot of a cluster.
func (s *Service) findings(ctx context.Context, v *cluster.View) ([]Finding, error) {
	id := v.Cluster.ID
	s.mu.Lock()
	e, ok := s.latest[id]
	s.mu.Unlock()
	if ok && e.viewAt.Equal(v.UpdatedAt) {
		return e.findings, nil
	}
	nodes, err := s.store.ListNodes(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("listing the nodes: %w", err)
	}
	findings := Evaluate(&Input{View: v, Nodes: nodes, Now: s.now()})
	s.mu.Lock()
	if cur, ok := s.latest[id]; !ok || !cur.viewAt.After(v.UpdatedAt) {
		s.latest[id] = evaluation{viewAt: v.UpdatedAt, findings: findings}
	}
	s.mu.Unlock()
	return findings, nil
}

// record writes when findings were seen: new findings at once, the others at most every
// seenEvery.
func (s *Service) record(ctx context.Context, clusterID string, findings []Finding) error {
	now := s.now()
	s.mu.Lock()
	prev := s.written[clusterID]
	s.mu.Unlock()
	next := make(map[string]time.Time, len(findings))
	var due []store.ProblemKey
	for _, f := range findings {
		if at, ok := prev[f.Fingerprint]; ok && now.Sub(at) < seenEvery {
			next[f.Fingerprint] = at
			continue
		}
		due = append(due, f.key())
		next[f.Fingerprint] = now
	}
	if err := s.store.SeeProblems(ctx, clusterID, due, now); err != nil {
		return err
	}
	s.mu.Lock()
	s.written[clusterID] = next
	s.mu.Unlock()
	return nil
}

// forget deletes the states of findings not seen for forgetAfter and drops the clusters that
// were deleted from memory; the states of deleted clusters go with them.
func (s *Service) forget(ctx context.Context) {
	if n, err := s.store.ForgetProblems(ctx, s.now().Add(-forgetAfter)); err != nil {
		s.log.Warn("deleting old problem states", "error", err)
	} else if n > 0 {
		s.log.Info("deleted the states of problems not seen for 30 days", "count", n)
	}
	clusters, err := s.store.ListClusters(ctx)
	if err != nil {
		return
	}
	keep := map[string]bool{}
	for _, c := range clusters {
		keep[c.ID] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.latest {
		if !keep[id] {
			delete(s.latest, id)
		}
	}
	for id := range s.written {
		if !keep[id] {
			delete(s.written, id)
		}
	}
}

// Problem is a finding with its state.
type Problem struct {
	Finding
	FirstSeen      time.Time  `json:"firstSeen"`
	LastSeen       time.Time  `json:"lastSeen"`
	AcknowledgedBy string     `json:"acknowledgedBy,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledgedAt,omitempty"`
	// MutedUntil is set while the problem is muted.
	MutedUntil *time.Time `json:"mutedUntil,omitempty"`
}

// withState combines a finding of the view updated at seen with its stored state.
func withState(f Finding, st *store.ProblemState, seen, now time.Time) Problem {
	p := Problem{Finding: f, FirstSeen: seen, LastSeen: seen}
	if st == nil {
		return p
	}
	if st.FirstSeen.Before(seen) {
		p.FirstSeen = st.FirstSeen
	}
	p.AcknowledgedBy, p.AcknowledgedAt = st.AcknowledgedBy, st.AcknowledgedAt
	if st.MutedUntil != nil && st.MutedUntil.After(now) {
		p.MutedUntil = st.MutedUntil
	}
	return p
}

// Problems returns the current problems of a cluster with their states and the time of the
// snapshot they come from.
func (s *Service) Problems(ctx context.Context, clusterID string) ([]Problem, time.Time, error) {
	v, err := s.clusters.View(ctx, clusterID, viewAge)
	if err != nil {
		return nil, time.Time{}, err
	}
	findings, err := s.findings(ctx, v)
	if err != nil {
		return nil, time.Time{}, err
	}
	states, err := s.store.ProblemStates(ctx, clusterID)
	if err != nil {
		return nil, time.Time{}, err
	}
	byFingerprint := make(map[string]*store.ProblemState, len(states))
	for _, st := range states {
		byFingerprint[st.Fingerprint] = st
	}
	now := s.now()
	out := make([]Problem, 0, len(findings))
	for _, f := range findings {
		out = append(out, withState(f, byFingerprint[f.Fingerprint], v.UpdatedAt, now))
	}
	return out, v.UpdatedAt, nil
}

// current returns the current finding of a cluster with a fingerprint.
func (s *Service) current(ctx context.Context, clusterID, fingerprint string) (Finding, error) {
	v, err := s.clusters.View(ctx, clusterID, viewAge)
	if err != nil {
		return Finding{}, err
	}
	findings, err := s.findings(ctx, v)
	if err != nil {
		return Finding{}, err
	}
	for _, f := range findings {
		if f.Fingerprint == fingerprint {
			return f, nil
		}
	}
	return Finding{}, errNotCurrent
}

// Counts are the problems of a cluster by severity; muted problems count only as muted.
type Counts struct {
	ClusterID   string    `json:"clusterId"`
	Critical    int       `json:"critical"`
	Warning     int       `json:"warning"`
	Info        int       `json:"info"`
	Muted       int       `json:"muted"`
	EvaluatedAt time.Time `json:"evaluatedAt"`
}

// Counts returns the counts of the problems of every cluster.
func (s *Service) Counts(ctx context.Context) ([]Counts, error) {
	views, err := s.clusters.Views(ctx, time.Minute)
	if err != nil {
		return nil, err
	}
	states, err := s.store.ProblemStates(ctx, "")
	if err != nil {
		return nil, err
	}
	now := s.now()
	muted := map[string]bool{}
	for _, st := range states {
		if st.MutedUntil != nil && st.MutedUntil.After(now) {
			muted[st.ClusterID+"/"+st.Fingerprint] = true
		}
	}
	out := make([]Counts, 0, len(views))
	for _, v := range views {
		if v.Cluster == nil {
			continue
		}
		findings, err := s.findings(ctx, v)
		if err != nil {
			return nil, err
		}
		c := Counts{ClusterID: v.Cluster.ID, EvaluatedAt: v.UpdatedAt}
		for _, f := range findings {
			switch {
			case muted[v.Cluster.ID+"/"+f.Fingerprint]:
				c.Muted++
			case f.Severity == SeverityCritical:
				c.Critical++
			case f.Severity == SeverityWarning:
				c.Warning++
			default:
				c.Info++
			}
		}
		out = append(out, c)
	}
	return out, nil
}
