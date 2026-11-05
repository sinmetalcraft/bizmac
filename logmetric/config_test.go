package logmetric_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sinmetalcraft/bizmac/logmetric"
)

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logmetric.yaml")
	src := `project: my-project
metrics:
  - name: app_error_count
    description: アプリが出した ERROR 以上のログの件数
    filter: >-
      resource.type="cloud_run_revision"
      AND severity>=ERROR
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := logmetric.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error: %v", err)
	}
	if len(f.Metrics) != 1 {
		t.Fatalf("Metrics = %d 件, want 1 件", len(f.Metrics))
	}
	if got, want := f.Metrics[0].Filter, `resource.type="cloud_run_revision" AND severity>=ERROR`; got != want {
		t.Errorf("Filter = %q, want %q", got, want)
	}
}

func TestLoadFileMissing(t *testing.T) {
	f, err := logmetric.LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadFile() error: %v", err)
	}
	if len(f.Metrics) != 0 {
		t.Errorf("Metrics = %d 件, want 0 件", len(f.Metrics))
	}
}

func TestValidate(t *testing.T) {
	counter := func() *logmetric.LogMetric {
		return &logmetric.LogMetric{Name: "m", Filter: "severity>=ERROR"}
	}
	distribution := func() *logmetric.LogMetric {
		m := counter()
		m.MetricDescriptor = &logmetric.MetricDescriptor{MetricKind: "DELTA", ValueType: "DISTRIBUTION"}
		m.ValueExtractor = "EXTRACT(jsonPayload.latency)"
		m.BucketOptions = &logmetric.BucketOptions{
			ExponentialBuckets: &logmetric.ExponentialBuckets{NumFiniteBuckets: 10, GrowthFactor: 2, Scale: 1},
		}
		return m
	}

	tests := []struct {
		name    string
		metric  func() *logmetric.LogMetric
		wantErr string
	}{
		{name: "counter", metric: counter},
		{name: "distribution", metric: distribution},
		{
			name: "階層のある名前",
			metric: func() *logmetric.LogMetric {
				m := counter()
				m.Name = "app/error_count"
				return m
			},
		},
		{
			name: "先頭の /",
			metric: func() *logmetric.LogMetric {
				m := counter()
				m.Name = "/app"
				return m
			},
			wantErr: "name は",
		},
		{
			name: "長すぎる名前",
			metric: func() *logmetric.LogMetric {
				m := counter()
				m.Name = strings.Repeat("a", 101)
				return m
			},
			wantErr: "name は",
		},
		{
			name: "filter が空",
			metric: func() *logmetric.LogMetric {
				m := counter()
				m.Filter = " "
				return m
			},
			wantErr: "filter is empty",
		},
		{
			name: "ラベルに無い label_extractors",
			metric: func() *logmetric.LogMetric {
				m := counter()
				m.LabelExtractors = map[string]string{"path": "EXTRACT(httpRequest.requestUrl)"}
				return m
			},
			wantErr: "label_extractors.path",
		},
		{
			name: "ラベルの重複",
			metric: func() *logmetric.LogMetric {
				m := counter()
				m.MetricDescriptor = &logmetric.MetricDescriptor{Labels: []*logmetric.LabelDescriptor{{Key: "a"}, {Key: "a"}}}
				return m
			},
			wantErr: "more than once",
		},
		{
			name: "カウンタに value_extractor",
			metric: func() *logmetric.LogMetric {
				m := counter()
				m.ValueExtractor = "EXTRACT(jsonPayload.latency)"
				return m
			},
			wantErr: "DISTRIBUTION にした場合だけ",
		},
		{
			name: "分布に bucket_options が無い",
			metric: func() *logmetric.LogMetric {
				m := distribution()
				m.BucketOptions = nil
				return m
			},
			wantErr: "bucket_options が必要",
		},
		{
			name: "bucket_options が 2 つ",
			metric: func() *logmetric.LogMetric {
				m := distribution()
				m.BucketOptions.LinearBuckets = &logmetric.LinearBuckets{NumFiniteBuckets: 10, Width: 1}
				return m
			},
			wantErr: "いずれか 1 つ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.metric().Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %v, want %q を含むエラー", err, tt.wantErr)
			}
		})
	}
}

func TestFileValidateDuplicate(t *testing.T) {
	f := &logmetric.File{Metrics: []*logmetric.LogMetric{
		{Name: "m", Filter: "severity>=ERROR"},
		{Name: "m", Filter: "severity>=ERROR"},
	}}
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Errorf("Validate() error = %v, want 重複のエラー", err)
	}
}
