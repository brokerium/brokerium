// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

// Package exposition collects the metrics of GET /metrics from the Enterprise features and
// writes them in the Prometheus text format. A feature adds its metrics with Register, for
// example the problems of every cluster by code and severity.
package exposition

import (
	"bytes"
	"context"
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// ContentType is the media type of the text format that Write produces.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// Types of metric families.
const (
	Gauge   = "gauge"
	Counter = "counter"
)

// Family is a metric family: a name, its help text and type, and the samples.
type Family struct {
	Name    string
	Help    string
	Type    string
	Samples []Sample
}

// Sample is a value of a family with its labels. The label sets of a family must differ.
type Sample struct {
	Labels []Label
	Value  float64
}

// Label is a label of a sample.
type Label struct {
	Name, Value string
}

// NewGauge returns a gauge family without samples.
func NewGauge(name, help string) *Family {
	return &Family{Name: name, Help: help, Type: Gauge}
}

// Add appends a sample; labels are pairs of a label name and its value.
func (f *Family) Add(value float64, labels ...string) {
	s := Sample{Value: value, Labels: make([]Label, 0, len(labels)/2)}
	for i := 0; i+1 < len(labels); i += 2 {
		s.Labels = append(s.Labels, Label{Name: labels[i], Value: labels[i+1]})
	}
	f.Samples = append(f.Samples, s)
}

// Collector returns the metric families of a feature. It runs on every scrape, so it reads the
// state the server keeps anyway instead of calling Kafka.
type Collector func(ctx context.Context) []*Family

// Registry holds the collectors of an endpoint.
type Registry struct {
	mu         sync.RWMutex
	collectors map[string]Collector
}

// Default is the registry of GET /metrics.
var Default = &Registry{}

// Register adds a collector to the registry of GET /metrics.
func Register(name string, c Collector) {
	Default.Register(name, c)
}

// Register adds a collector under a name; registering the name again replaces its collector.
func (r *Registry) Register(name string, c Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.collectors == nil {
		r.collectors = map[string]Collector{}
	}
	r.collectors[name] = c
}

// Gather runs the collectors in the order of their names and returns the families sorted by
// name; samples of families with the same name and type are merged.
func (r *Registry) Gather(ctx context.Context) []*Family {
	r.mu.RLock()
	names := make([]string, 0, len(r.collectors))
	for name := range r.collectors {
		names = append(names, name)
	}
	collectors := make([]Collector, 0, len(names))
	slices.Sort(names)
	for _, name := range names {
		collectors = append(collectors, r.collectors[name])
	}
	r.mu.RUnlock()
	byName := map[string]*Family{}
	var out []*Family
	for _, collect := range collectors {
		for _, f := range collect(ctx) {
			if f == nil {
				continue
			}
			if prev, ok := byName[f.Name]; ok {
				if prev.Type == f.Type {
					prev.Samples = append(prev.Samples, f.Samples...)
				}
				continue
			}
			merged := *f
			merged.Samples = slices.Clone(f.Samples)
			byName[f.Name] = &merged
			out = append(out, &merged)
		}
	}
	slices.SortFunc(out, func(a, b *Family) int { return strings.Compare(a.Name, b.Name) })
	return out
}

var (
	metricNameRe = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelNameRe  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// Valid reports whether a family can be written: valid names and a known type.
func (f *Family) Valid() bool {
	if !metricNameRe.MatchString(f.Name) || (f.Type != Gauge && f.Type != Counter) {
		return false
	}
	for _, s := range f.Samples {
		for _, l := range s.Labels {
			if !labelNameRe.MatchString(l.Name) || strings.HasPrefix(l.Name, "__") {
				return false
			}
		}
	}
	return true
}

// Write writes families in the Prometheus text format; invalid families are left out.
func Write(w io.Writer, families []*Family) error {
	var b bytes.Buffer
	for _, f := range families {
		if !f.Valid() || len(f.Samples) == 0 {
			continue
		}
		b.WriteString("# HELP " + f.Name + " " + escapeHelp(f.Help) + "\n")
		b.WriteString("# TYPE " + f.Name + " " + f.Type + "\n")
		for _, s := range f.Samples {
			b.WriteString(f.Name)
			if len(s.Labels) > 0 {
				b.WriteByte('{')
				for i, l := range s.Labels {
					if i > 0 {
						b.WriteByte(',')
					}
					b.WriteString(l.Name + `="` + escapeLabel(l.Value) + `"`)
				}
				b.WriteByte('}')
			}
			b.WriteString(" " + formatValue(s.Value) + "\n")
		}
	}
	_, err := b.WriteTo(w)
	return err
}

var (
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	labelEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
)

func escapeHelp(s string) string  { return helpEscaper.Replace(s) }
func escapeLabel(s string) string { return labelEscaper.Replace(s) }

func formatValue(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}
