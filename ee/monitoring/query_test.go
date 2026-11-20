// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 20, 12, 0, 7, 0, time.UTC)
	tests := []struct {
		query            string
		start, end       time.Time
		step, rate, rank time.Duration
		wantErr          string
	}{
		{query: "", start: now.Add(-time.Hour).Truncate(15 * time.Second), end: now.Truncate(15 * time.Second), step: 15 * time.Second,
			rate: 5 * time.Minute, rank: time.Hour},
		{query: "range=6h", start: time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC), end: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
			step: time.Minute, rate: 5 * time.Minute, rank: 6 * time.Hour},
		{query: "range=24h", start: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC), end: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
			step: 5 * time.Minute, rate: 5 * time.Minute, rank: 6 * time.Hour},
		{query: "range=7d", start: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC), end: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
			step: 30 * time.Minute, rate: 30 * time.Minute, rank: 6 * time.Hour},
		{query: "start=2026-11-20T10:00:00Z&end=2026-11-20T11:00:00Z&step=60", start: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
			end: time.Date(2026, 11, 20, 11, 0, 0, 0, time.UTC), step: time.Minute, rate: 5 * time.Minute, rank: time.Hour},
		{query: "start=1795168800&step=10m", start: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC), end: now, step: 10 * time.Minute,
			rate: 10 * time.Minute, rank: 2*time.Hour + 7*time.Second},
		{query: "start=2026-11-20T11:30:00Z", start: time.Date(2026, 11, 20, 11, 30, 0, 0, time.UTC), end: now, step: 15 * time.Second,
			rate: 5 * time.Minute, rank: 30*time.Minute + 7*time.Second},
		{query: "range=2h", wantErr: "range must be one of 1h, 6h, 24h, 7d"},
		{query: "end=2026-11-20T11:00:00Z", wantErr: "start: missing"},
		{query: "start=yesterday", wantErr: "RFC 3339 or Unix seconds"},
		{query: "start=2026-11-20T11:00:00Z&end=2026-11-20T10:00:00Z", wantErr: "end must be after start"},
		{query: "start=2026-01-01T00:00:00Z", wantErr: "at most 31 days"},
		{query: "start=2026-11-20T00:00:00Z&step=15s", wantErr: "2880 points per series"},
		{query: "start=2026-11-20T11:00:00Z&step=500ms", wantErr: "at least one second"},
		{query: "start=2026-11-20T11:00:00Z&step=often", wantErr: "seconds or a duration"},
	}
	for _, tt := range tests {
		q, err := url.ParseQuery(tt.query)
		if err != nil {
			t.Fatal(err)
		}
		w, err := parseWindow(q, now)
		if tt.wantErr != "" {
			if !errors.Is(err, ErrBadRange) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%q: error %v, want one containing %q", tt.query, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tt.query, err)
			continue
		}
		if !w.start.Equal(tt.start) || !w.end.Equal(tt.end) || w.step != tt.step || w.rate != tt.rate || w.rank != tt.rank {
			t.Errorf("%q: window %v..%v step %v rate %v rank %v; want %v..%v step %v rate %v rank %v", tt.query, w.start, w.end,
				w.step, w.rate, w.rank, tt.start, tt.end, tt.step, tt.rate, tt.rank)
		}
	}
}

// recordingQuerier answers every query with the series of a function and records the
// expressions.
type recordingQuerier struct {
	exprs  []string
	answer func(expr string) []Series
}

func (q *recordingQuerier) Query(_ context.Context, expr string, _ time.Time) (*Result, error) {
	q.exprs = append(q.exprs, expr)
	return &Result{Series: q.answer(expr)}, nil
}

func (q *recordingQuerier) QueryRange(_ context.Context, expr string, _, _ time.Time, _ time.Duration) (*Result, error) {
	q.exprs = append(q.exprs, expr)
	return &Result{Series: q.answer(expr), Warnings: []string{"w", "w"}}, nil
}

func TestPanels(t *testing.T) {
	t.Parallel()
	ids := map[string]bool{}
	placeholder := regexp.MustCompile(`\$[a-z]+`)
	known := []string{"$sel", "$rate", "$range", "$topics", "$heap", "$idle"}
	for _, p := range Panels {
		if ids[p.ID] || p.Title == "" || len(p.queries) == 0 {
			t.Errorf("panel %q: duplicate id, no title or no queries", p.ID)
		}
		ids[p.ID] = true
		switch p.Section {
		case SectionCluster, SectionBroker, SectionTopic:
		default:
			t.Errorf("panel %s: unknown section %q", p.ID, p.Section)
		}
		if (p.Section == SectionBroker) != (p.by == "instance") || (p.Section == SectionTopic) != (p.rank != "") {
			t.Errorf("panel %s: broker panels group by instance and topic panels rank topics", p.ID)
		}
		for _, expr := range append([]string{p.rank}, func() []string {
			var out []string
			for _, q := range p.queries {
				out = append(out, q.Expr)
			}
			return out
		}()...) {
			if expr == "" {
				continue
			}
			if !strings.Contains(expr, "{$sel") {
				t.Errorf("panel %s: %s does not select the cluster", p.ID, expr)
			}
			for _, ph := range placeholder.FindAllString(expr, -1) {
				if !slices.Contains(known, ph) {
					t.Errorf("panel %s: unknown placeholder %s", p.ID, ph)
				}
			}
			if strings.Count(expr, "(") != strings.Count(expr, ")") || strings.Count(expr, "{") != strings.Count(expr, "}") ||
				strings.Count(expr, "[") != strings.Count(expr, "]") || strings.Count(expr, `"`)%2 != 0 {
				t.Errorf("panel %s: unbalanced expression %s", p.ID, expr)
			}
		}
	}
	if got := panelsFor(nil); slices.ContainsFunc(got, func(p Panel) bool { return p.kraft }) {
		t.Error("KRaft panels without a profile")
	}
	if got := panelsFor(&Profile{KRaft: true}); len(got) != len(Panels) {
		t.Errorf("a KRaft profile shows %d of %d panels", len(got), len(Panels))
	}
	if _, ok := lookupPanel("broker-heap"); !ok {
		t.Error("lookupPanel misses broker-heap")
	}
	if _, ok := lookupPanel("nope"); ok {
		t.Error("lookupPanel found an unknown panel")
	}
}

func TestRunPanel(t *testing.T) {
	t.Parallel()
	w := window{start: time.Unix(1763636400, 0), end: time.Unix(1763640000, 0), step: 15 * time.Second, rate: 5 * time.Minute,
		rank: time.Hour}
	tg := target{selector: `job="kafka"`, nodes: clusterNodes(alphaView())}
	broker := func(instance string, v float64) Series {
		return Series{Labels: map[string]string{"instance": instance}, Points: []Point{{T: 1763636400, V: v}}}
	}

	heap, _ := lookupPanel("broker-heap")
	q := &recordingQuerier{answer: func(string) []Series {
		return []Series{broker("kafka-10.example.com:9141", 3), broker("kafka-2.example.com:9141", 2), broker("kafka-1.example.com:9141", 1)}
	}}
	data, err := runPanel(context.Background(), q, heap, tg, Profile{Exporter: Exporter0}, w)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{`sum by (instance) (jvm_memory_bytes_used{job="kafka",area="heap"})`}; !slices.Equal(q.exprs, want) {
		t.Errorf("queries = %q, want %q", q.exprs, want)
	}
	var names []string
	for _, s := range data.Series {
		names = append(names, s.Name)
	}
	if want := []string{"broker 101", "broker 102", "kafka-10.example.com:9141"}; !slices.Equal(names, want) {
		t.Errorf("series = %v, want %v", names, want)
	}
	if data.Unit != UnitBytes || data.Step != 15 || data.Start != 1763636400 || data.End != 1763640000 ||
		data.Series[0].Points[0] != [2]float64{1763636400, 1} || !slices.Equal(data.Warnings, []string{"w"}) {
		t.Errorf("data = %+v", data)
	}

	idle, _ := lookupPanel("broker-request-handler-idle")
	q.exprs = nil
	if _, err := runPanel(context.Background(), q, idle, tg, Profile{Exporter: Exporter1}, w); err != nil {
		t.Fatal(err)
	}
	if want := `100 * sum by (instance) (rate(kafka_server_kafkarequesthandlerpool_requesthandleravgidlepercent_count_total{job="kafka"}[300s])) / 1e9`; q.exprs[0] != want {
		t.Errorf("idle query = %s, want %s", q.exprs[0], want)
	}

	bytes, _ := lookupPanel("cluster-bytes")
	q = &recordingQuerier{answer: func(string) []Series { return []Series{{Labels: map[string]string{}, Points: []Point{{T: 1, V: 2}}}} }}
	if data, err = runPanel(context.Background(), q, bytes, tg, Profile{}, w); err != nil {
		t.Fatal(err)
	}
	if len(data.Series) != 2 || data.Series[0].Name != "in" || data.Series[1].Name != "out" ||
		q.exprs[1] != `sum(rate(kafka_server_brokertopicmetrics_bytesout_total{job="kafka",topic=""}[300s]))` {
		t.Errorf("cluster bytes: series %+v, queries %q", data.Series, q.exprs)
	}

	requests, _ := lookupPanel("cluster-requests")
	q = &recordingQuerier{answer: func(string) []Series {
		return []Series{{Labels: map[string]string{"request": "Produce"}}, {Labels: map[string]string{"request": "FetchConsumer"}}}
	}}
	if data, err = runPanel(context.Background(), q, requests, tg, Profile{}, w); err != nil {
		t.Fatal(err)
	}
	if data.Series[0].Name != "consumer fetch" || data.Series[1].Name != "produce" {
		t.Errorf("request series = %+v", data.Series)
	}
}

func TestRunTopPanel(t *testing.T) {
	t.Parallel()
	w := window{start: time.Unix(1763636400, 0), end: time.Unix(1763640000, 0), step: 15 * time.Second, rate: 5 * time.Minute,
		rank: time.Hour}
	tg := target{selector: `job="kafka"`}
	topic := func(name string, v float64) Series {
		return Series{Labels: map[string]string{"topic": name}, Points: []Point{{T: 1763640000, V: v}}}
	}
	p, _ := lookupPanel("topic-bytes-in")
	q := &recordingQuerier{answer: func(expr string) []Series {
		if strings.HasPrefix(expr, "topk(") {
			return []Series{topic("payments.v1", 10), topic("orders", 30), topic("audit", 20)}
		}
		return []Series{topic("audit", 1), topic("payments.v1", 1), topic("orders", 1)}
	}}
	data, err := runPanel(context.Background(), q, p, tg, Profile{}, w)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`topk(10, sum by (topic) (rate(kafka_server_brokertopicmetrics_bytesin_total{job="kafka",topic!=""}[3600s])))`,
		`sum by (topic) (rate(kafka_server_brokertopicmetrics_bytesin_total{job="kafka",topic=~"orders|audit|payments\\.v1"}[300s]))`,
	}
	if !slices.Equal(q.exprs, want) {
		t.Errorf("queries = %q\nwant %q", q.exprs, want)
	}
	var names []string
	for _, s := range data.Series {
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"orders", "audit", "payments.v1"}) {
		t.Errorf("series in the order of the ranking: %v", names)
	}

	q = &recordingQuerier{answer: func(string) []Series { return nil }}
	if data, err = runPanel(context.Background(), q, p, tg, Profile{}, w); err != nil || len(data.Series) != 0 || len(q.exprs) != 1 {
		t.Errorf("without ranked topics: %+v, %v, queries %q", data, err, q.exprs)
	}
}

func TestRunPanelTruncates(t *testing.T) {
	t.Parallel()
	p, _ := lookupPanel("broker-cpu")
	q := &recordingQuerier{answer: func(string) []Series {
		out := make([]Series, maxSeries+5)
		for i := range out {
			out[i] = Series{Labels: map[string]string{"instance": "kafka-" + strings.Repeat("x", i) + ":9141"}}
		}
		return out
	}}
	data, err := runPanel(context.Background(), q, p, target{selector: `job="kafka"`}, Profile{}, window{step: time.Minute})
	if err != nil || len(data.Series) != maxSeries || !data.Truncated {
		t.Errorf("%d series, truncated %v, %v", len(data.Series), data.Truncated, err)
	}
}

func TestHelpers(t *testing.T) {
	t.Parallel()
	if got := promDuration(90 * time.Minute); got != "5400s" {
		t.Errorf("promDuration = %s", got)
	}
	if got := alternation([]string{"a.b", `c"d`, "e"}); got != `a\\.b|c\"d|e` {
		t.Errorf("alternation = %s", got)
	}
	names := []string{"broker 10", "broker 9", "controller 1", "a", "broker 101"}
	slices.SortFunc(names, compareNames)
	if want := []string{"a", "broker 9", "broker 10", "broker 101", "controller 1"}; !slices.Equal(names, want) {
		t.Errorf("sorted names = %v, want %v", names, want)
	}
	if got := seriesName(map[string]string{"job": "kafka", "env": "prod"}, "", "", nodes{}); got != "env=prod, job=kafka" {
		t.Errorf("seriesName without a legend = %q", got)
	}
	if got := seriesName(map[string]string{}, "", "", nodes{}); got != "value" {
		t.Errorf("seriesName without labels = %q", got)
	}
	if got := Ranges(); !slices.Equal(got, []string{"1h", "6h", "24h", "7d"}) {
		t.Errorf("Ranges = %v", got)
	}
}
