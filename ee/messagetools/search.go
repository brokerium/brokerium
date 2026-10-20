// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package messagetools

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/brokerium/brokerium/internal/api"
	"github.com/brokerium/brokerium/internal/kafka"
)

// SearchRequest finds records of a topic.
type SearchRequest struct {
	Selection
	Filter
	// Limit is the number of matches to return: 100 by default, at most 1000.
	Limit int `json:"limit,omitempty"`
	// MaxRecords bounds the records read: a million by default.
	MaxRecords int64 `json:"maxRecords,omitempty"`
	// TimeoutSeconds bounds the time spent: 30 by default, at most 300.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

// SearchResult lists the matches, sorted by partition and offset, and how far the search got.
type SearchResult struct {
	Messages []api.Message `json:"messages"`
	ScanResult
}

const (
	defaultSearchLimit   = 100
	maxSearchLimit       = 1000
	defaultSearchRecords = 1_000_000
	defaultSearchTimeout = 30 * time.Second
	maxSearchTimeout     = 300 * time.Second
)

// bounded returns n, or def when n is not positive, capped at most.
func bounded[T int | int64 | time.Duration](n, def, most T) T {
	if n <= 0 {
		n = def
	}
	return min(n, most)
}

// Search reads the selected part of a topic and returns the records that match the filter.
func Search(ctx context.Context, adm *kadm.Client, profile kafka.Profile, topic string, req SearchRequest) (*SearchResult, error) {
	match, err := req.compile(req.FromTime, req.ToTime)
	if err != nil {
		return nil, err
	}
	ranges, err := Ranges(ctx, adm, topic, req.Selection)
	if err != nil {
		return nil, err
	}
	limit := bounded(req.Limit, defaultSearchLimit, maxSearchLimit)
	limits := Limits{
		Records: bounded(req.MaxRecords, defaultSearchRecords, 1<<40),
		Time:    bounded(time.Duration(req.TimeoutSeconds)*time.Second, defaultSearchTimeout, maxSearchTimeout),
	}
	out := &SearchResult{Messages: []api.Message{}}
	out.ScanResult, err = Scan(ctx, profile, topic, ranges, limits, func(rec *kgo.Record) bool {
		if match(rec) {
			out.Messages = append(out.Messages, api.NewMessage(rec))
		}
		return len(out.Messages) < limit
	})
	slices.SortFunc(out.Messages, func(a, b api.Message) int {
		return cmp.Or(cmp.Compare(a.Partition, b.Partition), cmp.Compare(a.Offset, b.Offset))
	})
	return out, err
}
