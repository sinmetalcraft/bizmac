package logmetric

import (
	"fmt"

	loggingapi "google.golang.org/api/logging/v2"
)

// ProjectPath は指標の親リソース名を組み立てる。
func ProjectPath(project string) string {
	return fmt.Sprintf("projects/%s", project)
}

// MetricPath は指標のフルリソース名を組み立てる。
func MetricPath(project, name string) string {
	return fmt.Sprintf("projects/%s/metrics/%s", project, name)
}

// FromAPI は API の LogMetric を yaml の定義へ変換する。
// metric_descriptor の name / type / description などの出力専用フィールドは落とす。
func FromAPI(api *loggingapi.LogMetric) *LogMetric {
	m := &LogMetric{
		Name:            api.Name,
		Description:     api.Description,
		Filter:          api.Filter,
		LabelExtractors: api.LabelExtractors,
		ValueExtractor:  api.ValueExtractor,
	}
	if d := api.MetricDescriptor; d != nil {
		md := &MetricDescriptor{
			MetricKind:  d.MetricKind,
			ValueType:   d.ValueType,
			Unit:        d.Unit,
			DisplayName: d.DisplayName,
		}
		for _, l := range d.Labels {
			md.Labels = append(md.Labels, &LabelDescriptor{
				Key:         l.Key,
				ValueType:   l.ValueType,
				Description: l.Description,
			})
		}
		m.MetricDescriptor = md
	}
	if b := api.BucketOptions; b != nil {
		bo := &BucketOptions{}
		if l := b.LinearBuckets; l != nil {
			bo.LinearBuckets = &LinearBuckets{NumFiniteBuckets: l.NumFiniteBuckets, Width: l.Width, Offset: l.Offset}
		}
		if e := b.ExponentialBuckets; e != nil {
			bo.ExponentialBuckets = &ExponentialBuckets{NumFiniteBuckets: e.NumFiniteBuckets, GrowthFactor: e.GrowthFactor, Scale: e.Scale}
		}
		if e := b.ExplicitBuckets; e != nil {
			bo.ExplicitBuckets = &ExplicitBuckets{Bounds: e.Bounds}
		}
		m.BucketOptions = bo
	}
	// export の出力をそろえるため、ラベルの並びと既定の型を正規化しておく。
	_ = m.Canonicalize()
	return m
}

// ApplyTo は yaml で管理しているフィールドを API の LogMetric に書き込む。
// update は全体を置き換える PUT なので、現在の指標に上書きして disabled や
// bucket_name などの管理していないフィールドを残すために使う。
// Create では空の LogMetric に書き込む。
func (m *LogMetric) ApplyTo(api *loggingapi.LogMetric) {
	api.Name = m.Name
	api.Description = m.Description
	api.Filter = m.Filter
	api.LabelExtractors = m.LabelExtractors
	api.ValueExtractor = m.ValueExtractor

	if d := m.MetricDescriptor; d != nil {
		if api.MetricDescriptor == nil {
			api.MetricDescriptor = &loggingapi.MetricDescriptor{}
		}
		ad := api.MetricDescriptor
		ad.MetricKind = d.MetricKind
		ad.ValueType = d.ValueType
		ad.Unit = d.Unit
		ad.DisplayName = d.DisplayName
		ad.Labels = nil
		for _, l := range d.Labels {
			ad.Labels = append(ad.Labels, &loggingapi.LabelDescriptor{
				Key:         l.Key,
				ValueType:   l.ValueType,
				Description: l.Description,
			})
		}
	} else if api.MetricDescriptor != nil {
		// yaml に書いていなければ既定値に任せる。ラベルは yaml のとおり無しにする。
		api.MetricDescriptor.Labels = nil
	}

	api.BucketOptions = nil
	if b := m.BucketOptions; b != nil {
		ab := &loggingapi.BucketOptions{}
		if l := b.LinearBuckets; l != nil {
			ab.LinearBuckets = &loggingapi.Linear{NumFiniteBuckets: l.NumFiniteBuckets, Width: l.Width, Offset: l.Offset}
		}
		if e := b.ExponentialBuckets; e != nil {
			ab.ExponentialBuckets = &loggingapi.Exponential{NumFiniteBuckets: e.NumFiniteBuckets, GrowthFactor: e.GrowthFactor, Scale: e.Scale}
		}
		if e := b.ExplicitBuckets; e != nil {
			ab.ExplicitBuckets = &loggingapi.Explicit{Bounds: e.Bounds}
		}
		api.BucketOptions = ab
	}
}
