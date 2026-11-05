package logmetric_test

import (
	"testing"

	"github.com/sinmetalcraft/bizmac/logmetric"
)

// apiMetric は Cloud Logging が既定値を埋めた状態の指標を作る。
func apiMetric(name, filter string) *logmetric.LogMetric {
	return &logmetric.LogMetric{
		Name:   name,
		Filter: filter,
		MetricDescriptor: &logmetric.MetricDescriptor{
			MetricKind: "DELTA",
			ValueType:  "INT64",
			Unit:       "1",
		},
	}
}

func TestBuildPlan(t *testing.T) {
	file := &logmetric.File{
		Project: "p",
		Metrics: []*logmetric.LogMetric{
			// metric_descriptor を書かなくても、既定値が埋まった実物と差分にならない。
			{Name: "same", Filter: "severity>=ERROR"},
			{Name: "changed", Filter: "severity>=WARNING"},
			{Name: "new", Filter: "severity>=ERROR"},
		},
	}
	actual := []*logmetric.LogMetric{
		apiMetric("changed", "severity>=ERROR"),
		apiMetric("old", "severity>=ERROR"),
		apiMetric("same", "severity>=ERROR"),
	}

	plan, err := logmetric.BuildPlan(file, actual)
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Create) != 1 || plan.Create[0].Name != "new" {
		t.Errorf("Create = %v, want [new]", plan.Create)
	}
	if len(plan.NoChange) != 1 || plan.NoChange[0].Name != "same" {
		t.Errorf("NoChange = %v, want [same]", plan.NoChange)
	}
	if len(plan.Delete) != 1 || plan.Delete[0].Name != "old" {
		t.Errorf("Delete = %v, want [old]", plan.Delete)
	}
	if len(plan.Update) != 1 {
		t.Fatalf("Update = %d 件, want 1 件", len(plan.Update))
	}
	u := plan.Update[0]
	if u.Name != "changed" || u.RecreateRequired {
		t.Errorf("Update = %s (RecreateRequired=%v), want changed (false)", u.Name, u.RecreateRequired)
	}
	if len(u.Changes) != 1 || u.Changes[0].Path != "filter" {
		t.Errorf("Changes = %v, want [filter]", u.Changes)
	}
	// 書かなかった既定値は現状の値で送る。
	if d := u.Desired.MetricDescriptor; d == nil || d.MetricKind != "DELTA" || d.ValueType != "INT64" || d.Unit != "1" {
		t.Errorf("Desired.MetricDescriptor = %+v, want DELTA / INT64 / 1", d)
	}
}

func TestBuildPlanLabels(t *testing.T) {
	file := &logmetric.File{
		Project: "p",
		Metrics: []*logmetric.LogMetric{{
			Name:   "m",
			Filter: "severity>=ERROR",
			MetricDescriptor: &logmetric.MetricDescriptor{
				// 並び順と、省略した STRING は差分にならない。
				Labels: []*logmetric.LabelDescriptor{{Key: "path"}, {Key: "code", ValueType: "INT64"}},
			},
		}},
	}
	got := apiMetric("m", "severity>=ERROR")
	got.MetricDescriptor.Labels = []*logmetric.LabelDescriptor{
		{Key: "code", ValueType: "INT64"},
		{Key: "path", ValueType: "STRING"},
	}

	plan, err := logmetric.BuildPlan(file, []*logmetric.LogMetric{got})
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.NoChange) != 1 {
		t.Errorf("NoChange = %d 件, want 1 件 (Update: %v)", len(plan.NoChange), plan.Update)
	}
}

func TestBuildPlanRecreate(t *testing.T) {
	file := &logmetric.File{
		Project: "p",
		Metrics: []*logmetric.LogMetric{{
			Name:             "m",
			Filter:           "severity>=ERROR",
			MetricDescriptor: &logmetric.MetricDescriptor{MetricKind: "DELTA", ValueType: "DISTRIBUTION"},
			ValueExtractor:   "EXTRACT(jsonPayload.latency)",
			BucketOptions: &logmetric.BucketOptions{
				ExplicitBuckets: &logmetric.ExplicitBuckets{Bounds: []float64{0.1, 1, 10}},
			},
		}},
	}

	plan, err := logmetric.BuildPlan(file, []*logmetric.LogMetric{apiMetric("m", "severity>=ERROR")})
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Update) != 1 || !plan.Update[0].RecreateRequired {
		t.Fatalf("Update = %v, want RecreateRequired", plan.Update)
	}
	u := plan.Update[0]
	if u.Actual.RecreateKey() != "DELTA/INT64" || u.Desired.RecreateKey() != "DELTA/DISTRIBUTION" {
		t.Errorf("RecreateKey = %s => %s, want DELTA/INT64 => DELTA/DISTRIBUTION",
			u.Actual.RecreateKey(), u.Desired.RecreateKey())
	}
}
