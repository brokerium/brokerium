// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package metrics estimates traffic without agents from consecutive snapshots (ADR 0010):
// message rates from end offsets, byte rates from partition size growth.
package metrics

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/bvaliev/brokerium/internal/kafka"
)

// RateEntry is a byte and message rate.
type RateEntry struct {
	BytesInPerSec    float64 `json:"bytesInPerSec"`
	MessagesInPerSec float64 `json:"messagesInPerSec"`
}

// Rates are the estimated rates of a cluster.
type Rates struct {
	SampledAt     time.Time            `json:"sampledAt"`
	WindowSeconds float64              `json:"windowSeconds"`
	Brokers       map[string]RateEntry `json:"brokers"`
	Topics        map[string]RateEntry `json:"topics"`
	// Partitions is keyed by "topic-partition"; not sent to the dashboard.
	Partitions map[string]RateEntry `json:"-"`
}

type point struct {
	at   time.Time
	size map[string]int64
	end  map[string]int64
}

// Sampler keeps a short history of snapshots per cluster.
type Sampler struct {
	mu     sync.Mutex
	window time.Duration
	series map[string][]point
	rates  map[string]*Rates
}

// NewSampler keeps samples for the given window.
func NewSampler(window time.Duration) *Sampler {
	return &Sampler{window: window, series: map[string][]point{}, rates: map[string]*Rates{}}
}

// Observe records a snapshot and recomputes rates.
func (s *Sampler) Observe(clusterID string, snap *kafka.Snapshot) {
	if snap == nil {
		return
	}
	p := point{at: snap.CollectedAt, size: map[string]int64{}, end: map[string]int64{}}
	replicas := map[string][]int32{}
	for _, t := range snap.Topics {
		for _, part := range t.Partitions {
			key := t.Name + "-" + strconv.Itoa(int(part.ID))
			p.size[key] = part.SizeBytes
			p.end[key] = part.EndOffset
			replicas[key] = part.Replicas
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pts := append(s.series[clusterID], p)
	for len(pts) > 2 && p.at.Sub(pts[0].at) > s.window {
		pts = pts[1:]
	}
	s.series[clusterID] = pts
	if len(pts) < 2 {
		return
	}
	first := pts[0]
	dt := p.at.Sub(first.at).Seconds()
	if dt <= 0 {
		return
	}
	r := &Rates{SampledAt: p.at, WindowSeconds: dt, Brokers: map[string]RateEntry{}, Topics: map[string]RateEntry{},
		Partitions: map[string]RateEntry{}}
	topicOf := map[string]string{}
	for _, t := range snap.Topics {
		for _, part := range t.Partitions {
			topicOf[t.Name+"-"+strconv.Itoa(int(part.ID))] = t.Name
		}
	}
	for key, end := range p.end {
		oldEnd, ok := first.end[key]
		if !ok {
			continue
		}
		msgs := float64(end-oldEnd) / dt
		if msgs < 0 {
			msgs = 0
		}
		bytes := float64(p.size[key]-first.size[key]) / dt
		if bytes <= 0 && msgs > 0 && end > 0 {
			bytes = msgs * float64(p.size[key]) / float64(end)
		}
		if bytes < 0 {
			bytes = 0
		}
		e := RateEntry{BytesInPerSec: bytes, MessagesInPerSec: msgs}
		r.Partitions[key] = e
		t := r.Topics[topicOf[key]]
		t.BytesInPerSec += bytes
		t.MessagesInPerSec += msgs
		r.Topics[topicOf[key]] = t
		for _, b := range replicas[key] {
			id := fmt.Sprint(b)
			be := r.Brokers[id]
			be.BytesInPerSec += bytes
			be.MessagesInPerSec += msgs
			r.Brokers[id] = be
		}
	}
	s.rates[clusterID] = r
}

// Rates returns the latest estimates of a cluster, or nil before two samples exist.
func (s *Sampler) Rates(clusterID string) *Rates {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rates[clusterID]
}
