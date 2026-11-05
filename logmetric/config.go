// Package logmetric は Cloud Logging のログベースの指標を yaml で管理するための
// 設定ファイルの読み書きと、Google Cloud との差分計算を提供する。
//
// 対象はユーザー定義の指標だけで、logging.googleapis.com/ の組み込みの指標は扱わない。
// ロケーションを持たないので、プロジェクト単位で管理する。
package logmetric

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultFileName は logmetric リソースの既定の設定ファイル名。
const DefaultFileName = "logmetric.yaml"

// 指標の種類と値の型。省略すると Cloud Logging が DELTA / INT64 を埋める。
const (
	MetricKindDelta       = "DELTA"
	ValueTypeInt64        = "INT64"
	ValueTypeDistribution = "DISTRIBUTION"
	// LabelValueTypeString はラベルの既定の型。REST のレスポンスでは省かれる。
	LabelValueTypeString = "STRING"
)

// File は logmetric.yaml 1 ファイル分の内容。
// ログベースの指標はロケーションを持たないので location は無い。
type File struct {
	Project string `yaml:"project,omitempty"`
	// IgnoreChange は全指標に適用される、変更を無視するプロパティパス。
	IgnoreChange []string     `yaml:"ignore_change,omitempty"`
	Metrics      []*LogMetric `yaml:"metrics"`
}

// LogMetric はログベースの指標 1 件。Logging API v2 の LogMetric に対応する。
// 出力専用のフィールドと、今は扱わない disabled / bucket_name / version は持たない。
type LogMetric struct {
	// Name は指標 ID。作る側が決める名前で、フルリソース名ではない。
	Name string `yaml:"name"`
	// IgnoreChange はこの指標にだけ追加で適用される無視プロパティパス。
	IgnoreChange []string `yaml:"ignore_change,omitempty"`

	Description string `yaml:"description,omitempty"`
	// Filter は数える対象のログを選ぶ Logging のクエリ。
	Filter           string            `yaml:"filter"`
	MetricDescriptor *MetricDescriptor `yaml:"metric_descriptor,omitempty"`
	// LabelExtractors はラベルのキー → ログから値を取り出す式 (EXTRACT / REGEXP_EXTRACT)。
	LabelExtractors map[string]string `yaml:"label_extractors,omitempty"`
	// ValueExtractor は分布の指標の値を取り出す式。
	ValueExtractor string         `yaml:"value_extractor,omitempty"`
	BucketOptions  *BucketOptions `yaml:"bucket_options,omitempty"`
}

// MetricDescriptor は指標の種類と型、ラベル。
// name / type / description は出力専用なので持たない。
type MetricDescriptor struct {
	// MetricKind は DELTA など。省略すると DELTA になる。
	MetricKind string `yaml:"metric_kind,omitempty"`
	// ValueType は INT64 / DISTRIBUTION など。省略すると INT64 になる。
	ValueType   string `yaml:"value_type,omitempty"`
	Unit        string `yaml:"unit,omitempty"`
	DisplayName string `yaml:"display_name,omitempty"`
	// Labels は指標のラベル。順序に意味が無いので key 順にそろえて比較する。
	Labels []*LabelDescriptor `yaml:"labels,omitempty"`
}

// LabelDescriptor は指標のラベル 1 つ。
type LabelDescriptor struct {
	Key string `yaml:"key"`
	// ValueType は STRING / BOOL / INT64。省略すると STRING になる。
	ValueType   string `yaml:"value_type,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// BucketOptions は分布の指標のバケット。3 種類のうち 1 つだけを書く。
type BucketOptions struct {
	LinearBuckets      *LinearBuckets      `yaml:"linear_buckets,omitempty"`
	ExponentialBuckets *ExponentialBuckets `yaml:"exponential_buckets,omitempty"`
	ExplicitBuckets    *ExplicitBuckets    `yaml:"explicit_buckets,omitempty"`
}

// LinearBuckets は幅が一定のバケット。
type LinearBuckets struct {
	NumFiniteBuckets int64   `yaml:"num_finite_buckets"`
	Width            float64 `yaml:"width"`
	Offset           float64 `yaml:"offset,omitempty"`
}

// ExponentialBuckets は幅が指数的に広がるバケット。
type ExponentialBuckets struct {
	NumFiniteBuckets int64   `yaml:"num_finite_buckets"`
	GrowthFactor     float64 `yaml:"growth_factor"`
	Scale            float64 `yaml:"scale"`
}

// ExplicitBuckets は境界を列挙したバケット。
type ExplicitBuckets struct {
	Bounds []float64 `yaml:"bounds"`
}

// namePattern は指標 ID の書式。API の LogMetric.name の説明に従う。
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_\-.,+!*'()%][A-Za-z0-9_\-.,+!*'()%/]{0,99}$`)

// LoadFile は設定ファイルを読み込む。ファイルが無い場合は空の File を返す。
func LoadFile(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &File{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("logmetric: read %s: %w", path, err)
	}
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("logmetric: parse %s: %w", path, err)
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("logmetric: %s: %w", path, err)
	}
	return &f, nil
}

// Validate は設定ファイルの内容を検証する。
func (f *File) Validate() error {
	seen := map[string]struct{}{}
	for i, m := range f.Metrics {
		if m == nil {
			return fmt.Errorf("metrics[%d] is empty", i)
		}
		if m.Name == "" {
			return fmt.Errorf("metrics[%d] has no name", i)
		}
		if _, ok := seen[m.Name]; ok {
			return fmt.Errorf("metric %q is defined more than once", m.Name)
		}
		seen[m.Name] = struct{}{}
		if err := m.Validate(); err != nil {
			return fmt.Errorf("metric %q: %w", m.Name, err)
		}
	}
	return nil
}

// Validate は指標 1 件の内容を検証する。
func (m *LogMetric) Validate() error {
	if !namePattern.MatchString(m.Name) {
		return fmt.Errorf("name は 100 文字以内で、英数字と _-.,+!*'()%%/ だけを使えます (先頭に / は書けません)")
	}
	if strings.TrimSpace(m.Filter) == "" {
		return fmt.Errorf("filter is empty")
	}

	labels := map[string]struct{}{}
	if d := m.MetricDescriptor; d != nil {
		for i, l := range d.Labels {
			if l == nil || l.Key == "" {
				return fmt.Errorf("metric_descriptor.labels[%d] has no key", i)
			}
			if _, ok := labels[l.Key]; ok {
				return fmt.Errorf("metric_descriptor.labels: key %q is defined more than once", l.Key)
			}
			labels[l.Key] = struct{}{}
		}
	}
	for key := range m.LabelExtractors {
		if _, ok := labels[key]; !ok {
			return fmt.Errorf("label_extractors.%s: metric_descriptor.labels にキー %q を書いてください", key, key)
		}
	}

	distribution := m.MetricDescriptor != nil && m.MetricDescriptor.ValueType == ValueTypeDistribution
	if distribution {
		if m.ValueExtractor == "" {
			return fmt.Errorf("value_type が DISTRIBUTION の場合は value_extractor が必要です")
		}
		if m.BucketOptions == nil {
			return fmt.Errorf("value_type が DISTRIBUTION の場合は bucket_options が必要です")
		}
	} else if m.ValueExtractor != "" || m.BucketOptions != nil {
		return fmt.Errorf("value_extractor / bucket_options は metric_descriptor.value_type を DISTRIBUTION にした場合だけ書けます")
	}
	if b := m.BucketOptions; b != nil {
		n := 0
		for _, set := range []bool{b.LinearBuckets != nil, b.ExponentialBuckets != nil, b.ExplicitBuckets != nil} {
			if set {
				n++
			}
		}
		if n != 1 {
			return fmt.Errorf("bucket_options には linear_buckets / exponential_buckets / explicit_buckets のいずれか 1 つを書いてください")
		}
	}
	return nil
}

// SortMetrics は指標を名前順に並べ替える。export の出力を安定させるために使う。
func (f *File) SortMetrics() {
	sort.Slice(f.Metrics, func(i, k int) bool { return f.Metrics[i].Name < f.Metrics[k].Name })
}

// Save は設定ファイルを書き出す。path が "-" の場合は標準出力へ書く。
func (f *File) Save(path string) error {
	b, err := f.Marshal()
	if err != nil {
		return err
	}
	if path == "-" {
		_, err := os.Stdout.Write(b)
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("logmetric: mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("logmetric: write %s: %w", path, err)
	}
	return nil
}

// Marshal は設定ファイルを yaml へエンコードする。
func (f *File) Marshal() ([]byte, error) {
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, fmt.Errorf("logmetric: encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("logmetric: encode yaml: %w", err)
	}
	return []byte(sb.String()), nil
}

// ItemName は resource.Item の実装。
func (m *LogMetric) ItemName() string { return m.Name }

// ItemIgnoreChange は resource.Item の実装。
func (m *LogMetric) ItemIgnoreChange() []string { return m.IgnoreChange }

// SetItemIgnoreChange は resource.Item の実装。
func (m *LogMetric) SetItemIgnoreChange(paths []string) { m.IgnoreChange = paths }

// Canonicalize は resource.Item の実装。
// ラベルを key 順にそろえ、省略されたラベルの型を STRING で埋める。
func (m *LogMetric) Canonicalize() error {
	d := m.MetricDescriptor
	if d == nil {
		return nil
	}
	for _, l := range d.Labels {
		if l.ValueType == "" {
			l.ValueType = LabelValueTypeString
		}
	}
	sort.Slice(d.Labels, func(i, k int) bool { return d.Labels[i].Key < d.Labels[k].Key })
	return nil
}

// RecreateKey は resource.Item の実装。
// 指標の種類と値の型は作成後に変えられないので、変わったら作り直しが必要になる。
func (m *LogMetric) RecreateKey() string {
	kind, typ := MetricKindDelta, ValueTypeInt64
	if d := m.MetricDescriptor; d != nil {
		if d.MetricKind != "" {
			kind = d.MetricKind
		}
		if d.ValueType != "" {
			typ = d.ValueType
		}
	}
	return kind + "/" + typ
}

// serverDefaultPaths は Cloud Logging が既定値を埋めるプロパティ。
// yaml に書かなかった場合は Google Cloud 側の値をそのまま使い、差分にも出さない。
var serverDefaultPaths = []string{
	"metric_descriptor.metric_kind",
	"metric_descriptor.value_type",
	"metric_descriptor.unit",
}

// ServerDefaultPaths は resource.ServerDefaults の実装。
func (m *LogMetric) ServerDefaultPaths() []string { return serverDefaultPaths }

// GetProject は resource.File の実装。
func (f *File) GetProject() string { return f.Project }

// SetProject は resource.File の実装。
func (f *File) SetProject(project string) { f.Project = project }

// GetLocation は resource.File の実装。ログベースの指標はロケーションを持たない。
func (f *File) GetLocation() string { return "" }

// SetLocation は resource.File の実装。ロケーションを持たないので何もしない。
func (f *File) SetLocation(string) {}

// GetIgnoreChange は resource.File の実装。
func (f *File) GetIgnoreChange() []string { return f.IgnoreChange }

// SetIgnoreChange は resource.File の実装。
func (f *File) SetIgnoreChange(paths []string) { f.IgnoreChange = paths }

// GetItems は resource.File の実装。
func (f *File) GetItems() []*LogMetric { return f.Metrics }

// SetItems は resource.File の実装。
func (f *File) SetItems(metrics []*LogMetric) { f.Metrics = metrics }

// Sort は resource.File の実装。
func (f *File) Sort() { f.SortMetrics() }
