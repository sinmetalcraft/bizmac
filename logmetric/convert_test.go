package logmetric_test

import (
	"reflect"
	"testing"

	"github.com/sinmetalcraft/bizmac/logmetric"
	loggingapi "google.golang.org/api/logging/v2"
)

func TestFromAPI(t *testing.T) {
	api := &loggingapi.LogMetric{
		Name:        "app_error_count",
		Description: "errors",
		Filter:      "severity>=ERROR",
		CreateTime:  "2026-11-01T00:00:00Z",
		MetricDescriptor: &loggingapi.MetricDescriptor{
			Name:        "projects/p/metricDescriptors/logging.googleapis.com/user/app_error_count",
			Type:        "logging.googleapis.com/user/app_error_count",
			Description: "errors",
			MetricKind:  "DELTA",
			ValueType:   "INT64",
			Unit:        "1",
			Labels: []*loggingapi.LabelDescriptor{
				{Key: "path"},
				{Key: "code", ValueType: "INT64"},
			},
		},
		LabelExtractors: map[string]string{"path": "EXTRACT(httpRequest.requestUrl)"},
	}

	got := logmetric.FromAPI(api)
	want := &logmetric.LogMetric{
		Name:        "app_error_count",
		Description: "errors",
		Filter:      "severity>=ERROR",
		MetricDescriptor: &logmetric.MetricDescriptor{
			MetricKind: "DELTA",
			ValueType:  "INT64",
			Unit:       "1",
			// key 順に並び、省略された型は STRING で埋まる。
			Labels: []*logmetric.LabelDescriptor{
				{Key: "code", ValueType: "INT64"},
				{Key: "path", ValueType: "STRING"},
			},
		},
		LabelExtractors: map[string]string{"path": "EXTRACT(httpRequest.requestUrl)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FromAPI() = %+v, want %+v", got, want)
	}
}

func TestApplyToKeepsUnmanagedFields(t *testing.T) {
	current := &loggingapi.LogMetric{
		Name:       "m",
		Filter:     "severity>=ERROR",
		Disabled:   true,
		BucketName: "projects/p/locations/global/buckets/b",
		MetricDescriptor: &loggingapi.MetricDescriptor{
			MetricKind:  "DELTA",
			ValueType:   "INT64",
			LaunchStage: "GA",
			Labels:      []*loggingapi.LabelDescriptor{{Key: "old"}},
		},
		ValueExtractor: "EXTRACT(jsonPayload.x)",
	}
	m := &logmetric.LogMetric{
		Name:   "m",
		Filter: "severity>=WARNING",
		MetricDescriptor: &logmetric.MetricDescriptor{
			MetricKind: "DELTA",
			ValueType:  "INT64",
			Labels:     []*logmetric.LabelDescriptor{{Key: "path", ValueType: "STRING"}},
		},
	}
	m.ApplyTo(current)

	if current.Filter != "severity>=WARNING" {
		t.Errorf("Filter = %q, want severity>=WARNING", current.Filter)
	}
	if !current.Disabled || current.BucketName == "" {
		t.Errorf("disabled / bucket_name が消えた: %+v", current)
	}
	if current.MetricDescriptor.LaunchStage != "GA" {
		t.Errorf("LaunchStage = %q, want GA", current.MetricDescriptor.LaunchStage)
	}
	if len(current.MetricDescriptor.Labels) != 1 || current.MetricDescriptor.Labels[0].Key != "path" {
		t.Errorf("Labels = %+v, want [path]", current.MetricDescriptor.Labels)
	}
	// yaml から外したフィールドは消える。
	if current.ValueExtractor != "" {
		t.Errorf("ValueExtractor = %q, want 空", current.ValueExtractor)
	}
}
