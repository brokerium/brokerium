// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Limits of panel queries.
const (
	maxPoints  = 1000
	maxSeries  = 50
	maxSpan    = 31 * 24 * time.Hour
	minRate    = 5 * time.Minute
	maxRank    = 6 * time.Hour
	autoPoints = 300
	minStep    = 15 * time.Second
)

// ErrBadRange is returned for a time range that cannot be queried.
var ErrBadRange = errors.New("invalid time range")

// preset is a time range of the range selector of the tab.
type preset struct {
	name       string
	span, step time.Duration
}

var presets = []preset{
	{"1h", time.Hour, 15 * time.Second},
	{"6h", 6 * time.Hour, time.Minute},
	{"24h", 24 * time.Hour, 5 * time.Minute},
	{"7d", 7 * 24 * time.Hour, 30 * time.Minute},
}

// Ranges lists the names of the preset time ranges.
func Ranges() []string {
	out := make([]string, len(presets))
	for i, p := range presets {
		out[i] = p.name
	}
	return out
}

// window is the time range of a panel query.
type window struct {
	start, end time.Time
	step       time.Duration
	// rate is the window of rate(), rank the window that ranks the topics of top-N panels.
	rate, rank time.Duration
}

// parseWindow reads range (1h, 6h, 24h or 7d), or start, end and step: times in RFC 3339 or
// Unix seconds and a step in seconds or as a duration such as 30s.
func parseWindow(q url.Values, now time.Time) (window, error) {
	var w window
	if q.Get("start") == "" && q.Get("end") == "" && q.Get("step") == "" {
		name := q.Get("range")
		if name == "" {
			name = presets[0].name
		}
		i := slices.IndexFunc(presets, func(p preset) bool { return p.name == name })
		if i < 0 {
			return w, fmt.Errorf("%w: range must be one of %s", ErrBadRange, strings.Join(Ranges(), ", "))
		}
		p := presets[i]
		w.step = p.step
		w.end = now.Truncate(p.step)
		w.start = w.end.Add(-p.span)
	} else {
		var err error
		if w.start, err = parseTime(q.Get("start")); err != nil {
			return w, fmt.Errorf("%w: start: %v", ErrBadRange, err)
		}
		w.end = now
		if q.Get("end") != "" {
			if w.end, err = parseTime(q.Get("end")); err != nil {
				return w, fmt.Errorf("%w: end: %v", ErrBadRange, err)
			}
		}
		span := w.end.Sub(w.start)
		switch {
		case span <= 0:
			return w, fmt.Errorf("%w: end must be after start", ErrBadRange)
		case span > maxSpan:
			return w, fmt.Errorf("%w: at most %d days", ErrBadRange, int(maxSpan.Hours()/24))
		}
		if q.Get("step") == "" {
			w.step = max(minStep, (span / autoPoints).Round(time.Second))
		} else if w.step, err = parseStep(q.Get("step")); err != nil {
			return w, fmt.Errorf("%w: step: %v", ErrBadRange, err)
		}
		if points := span / w.step; points > maxPoints {
			return w, fmt.Errorf("%w: %d points per series, at most %d; choose a larger step", ErrBadRange, points, maxPoints)
		}
	}
	w.rate = max(w.step, minRate)
	w.rank = min(w.end.Sub(w.start), maxRank)
	return w, nil
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("missing")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return time.Time{}, errors.New("must be RFC 3339 or Unix seconds")
	}
	return time.UnixMilli(int64(f * 1000)), nil
}

func parseStep(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return 0, errors.New("must be seconds or a duration such as 30s")
		}
		d = time.Duration(f * float64(time.Second))
	}
	if d < time.Second {
		return 0, errors.New("must be at least one second")
	}
	return d, nil
}

// promDuration renders a duration for PromQL in whole seconds.
func promDuration(d time.Duration) string {
	return strconv.FormatInt(int64(d/time.Second), 10) + "s"
}

// promStringEscaper escapes text for a PromQL string in double quotes.
var promStringEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

// alternation is a regular expression, escaped for a PromQL string, that matches the values.
func alternation(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = regexp.QuoteMeta(v)
	}
	return promStringEscaper.Replace(strings.Join(parts, "|"))
}

// PanelSeries is a series of a panel; points are pairs of Unix seconds and a value.
type PanelSeries struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	Points [][2]float64      `json:"points"`
}

// PanelData is the answer to a panel query.
type PanelData struct {
	Panel     string        `json:"panel"`
	Unit      string        `json:"unit"`
	Start     int64         `json:"start"`
	End       int64         `json:"end"`
	Step      int64         `json:"step"`
	Series    []PanelSeries `json:"series"`
	Truncated bool          `json:"truncated"`
	Warnings  []string      `json:"warnings,omitempty"`
}

// target is what a cluster is in Prometheus: its selector and the names of its nodes.
type target struct {
	selector string
	nodes    nodes
}

// querier runs PromQL queries; *Client is one.
type querier interface {
	Query(ctx context.Context, expr string, at time.Time) (*Result, error)
	QueryRange(ctx context.Context, expr string, start, end time.Time, step time.Duration) (*Result, error)
}

// runPanel runs the queries of a panel for a cluster.
func runPanel(ctx context.Context, q querier, p Panel, t target, prof Profile, w window) (*PanelData, error) {
	data := &PanelData{Panel: p.ID, Unit: p.Unit, Start: w.start.Unix(), End: w.end.Unix(), Step: int64(w.step / time.Second),
		Series: []PanelSeries{}}
	vars := []string{"$sel", t.selector, "$rate", promDuration(w.rate), "$range", promDuration(w.rank),
		"$heap", prof.heapMetric(), "$idle", prof.idleMetric()}
	var order []string
	if p.rank != "" {
		res, err := q.Query(ctx, strings.NewReplacer(vars...).Replace(p.rank), w.end)
		if err != nil {
			return nil, err
		}
		data.Warnings = append(data.Warnings, res.Warnings...)
		ranked := slices.Clone(res.Series)
		slices.SortStableFunc(ranked, func(a, b Series) int { return compareValue(b, a) })
		for _, s := range ranked {
			if v := s.Labels[p.by]; v != "" && !slices.Contains(order, v) {
				order = append(order, v)
			}
		}
		if len(order) == 0 {
			return data, nil
		}
		vars = append(vars, "$topics", alternation(order))
	}
	expand := strings.NewReplacer(vars...)
	for _, query := range p.queries {
		res, err := q.QueryRange(ctx, expand.Replace(query.Expr), w.start, w.end, w.step)
		if err != nil {
			return nil, err
		}
		data.Warnings = append(data.Warnings, res.Warnings...)
		for _, s := range res.Series {
			points := make([][2]float64, len(s.Points))
			for i, pt := range s.Points {
				points[i] = [2]float64{pt.T, pt.V}
			}
			data.Series = append(data.Series, PanelSeries{Name: seriesName(s.Labels, p.by, query.Legend, t.nodes),
				Labels: s.Labels, Points: points})
		}
	}
	if len(order) > 0 {
		slices.SortStableFunc(data.Series, func(a, b PanelSeries) int {
			return slices.Index(order, a.Labels[p.by]) - slices.Index(order, b.Labels[p.by])
		})
	} else if p.by != "" {
		slices.SortStableFunc(data.Series, func(a, b PanelSeries) int { return compareNames(a.Name, b.Name) })
	}
	if len(data.Series) > maxSeries {
		data.Series, data.Truncated = data.Series[:maxSeries], true
	}
	data.Warnings = slices.Compact(data.Warnings)
	return data, nil
}

func compareValue(a, b Series) int {
	value := func(s Series) float64 {
		if len(s.Points) == 0 {
			return 0
		}
		return s.Points[len(s.Points)-1].V
	}
	switch va, vb := value(a), value(b); {
	case va < vb:
		return -1
	case va > vb:
		return 1
	}
	return 0
}

var trailingNumberRe = regexp.MustCompile(`^(.*?)(\d+)$`)

// compareNames orders names with a trailing number by that number, so broker 9 comes before
// broker 10.
func compareNames(a, b string) int {
	ma, mb := trailingNumberRe.FindStringSubmatch(a), trailingNumberRe.FindStringSubmatch(b)
	if ma != nil && mb != nil && ma[1] == mb[1] {
		na, _ := strconv.Atoi(ma[2])
		nb, _ := strconv.Atoi(mb[2])
		if na != nb {
			return na - nb
		}
	}
	return strings.Compare(a, b)
}

// requestNames are the names of the request types of the request panels.
var requestNames = map[string]string{"Produce": "produce", "FetchConsumer": "consumer fetch", "FetchFollower": "follower fetch"}

// seriesName names a series by the label of its panel, or by the legend of its query.
func seriesName(labels map[string]string, by, legend string, n nodes) string {
	switch v := labels[by]; {
	case by == "instance" && v != "":
		return n.instanceName(v)
	case by == "request" && requestNames[v] != "":
		return requestNames[v]
	case by != "" && v != "":
		return v
	case legend != "":
		return legend
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	if len(parts) == 0 {
		return "value"
	}
	return strings.Join(parts, ", ")
}
