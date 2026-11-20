// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package exposition

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestWrite(t *testing.T) {
	t.Parallel()
	health := NewGauge("brokerium_cluster_health_status", "Health of a cluster:\n0 ok, 1 warning, 2 critical. A \\ stays.")
	health.Add(0, "cluster", "alpha")
	health.Add(2, "cluster", `we"ird\name`+"\n")
	special := NewGauge("brokerium_values", "Special values.")
	special.Add(math.NaN())
	special.Add(math.Inf(1), "kind", "plus")
	special.Add(math.Inf(-1), "kind", "minus")
	special.Add(1.5e9, "kind", "large")
	families := []*Family{
		health,
		special,
		NewGauge("brokerium_empty", "Families without samples are left out."),
		{Name: "brokerium bad name", Type: Gauge, Samples: []Sample{{Value: 1}}},
		{Name: "brokerium_bad_type", Type: "histogram", Samples: []Sample{{Value: 1}}},
		{Name: "brokerium_bad_label", Type: Gauge, Samples: []Sample{{Labels: []Label{{Name: "problem-code", Value: "x"}}, Value: 1}}},
		{Name: "brokerium_reserved_label", Type: Gauge, Samples: []Sample{{Labels: []Label{{Name: "__name__", Value: "x"}}, Value: 1}}},
	}
	var b strings.Builder
	if err := Write(&b, families); err != nil {
		t.Fatal(err)
	}
	want := `# HELP brokerium_cluster_health_status Health of a cluster:\n0 ok, 1 warning, 2 critical. A \\ stays.
# TYPE brokerium_cluster_health_status gauge
brokerium_cluster_health_status{cluster="alpha"} 0
brokerium_cluster_health_status{cluster="we\"ird\\name\n"} 2
# HELP brokerium_values Special values.
# TYPE brokerium_values gauge
brokerium_values NaN
brokerium_values{kind="plus"} +Inf
brokerium_values{kind="minus"} -Inf
brokerium_values{kind="large"} 1.5e+09
`
	if b.String() != want {
		t.Errorf("Write =\n%s\nwant\n%s", b.String(), want)
	}
}

func TestRegistry(t *testing.T) {
	t.Parallel()
	var r Registry
	if got := r.Gather(context.Background()); len(got) != 0 {
		t.Fatalf("an empty registry gathered %d families", len(got))
	}
	gauge := func(name string, value float64, labels ...string) *Family {
		f := NewGauge(name, "help of "+name)
		f.Add(value, labels...)
		return f
	}
	r.Register("problems", func(context.Context) []*Family {
		return []*Family{gauge("brokerium_cluster_problems", 3, "cluster", "alpha", "code", "topic-rf-below-min-isr", "severity", "critical"), nil}
	})
	r.Register("clusters", func(context.Context) []*Family {
		return []*Family{gauge("brokerium_cluster_reachable", 1, "cluster", "alpha"), gauge("brokerium_cluster_problems", 1, "cluster", "beta",
			"code", "under-replicated", "severity", "warning")}
	})
	r.Register("other", func(context.Context) []*Family {
		return []*Family{{Name: "brokerium_cluster_reachable", Type: Counter, Samples: []Sample{{Value: 7}}}}
	})
	got := r.Gather(context.Background())
	if len(got) != 2 || got[0].Name != "brokerium_cluster_problems" || got[1].Name != "brokerium_cluster_reachable" {
		t.Fatalf("Gather = %+v", got)
	}
	if problems := got[0].Samples; len(problems) != 2 || problems[0].Value != 1 || problems[1].Value != 3 {
		t.Errorf("merged problems = %+v, want the samples of clusters before those of problems", problems)
	}
	if reachable := got[1]; reachable.Type != Gauge || len(reachable.Samples) != 1 {
		t.Errorf("a family of another type was merged: %+v", reachable)
	}
	r.Register("problems", func(context.Context) []*Family { return nil })
	if got := r.Gather(context.Background()); len(got) != 2 || len(got[0].Samples) != 1 {
		t.Errorf("registering a name again did not replace its collector: %+v", got)
	}
}
