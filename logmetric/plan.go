package logmetric

import (
	"github.com/sinmetalcraft/bizmac/resource"
)

// Plan は yaml と Google Cloud の現状を突き合わせた結果。
type Plan = resource.Plan[*LogMetric]

// BuildPlan は yaml の定義 (file) と Google Cloud の現状 (actual) を比較して Plan を作る。
func BuildPlan(file *File, actual []*LogMetric) (*Plan, error) {
	return resource.BuildPlan(file.Project, "", file.IgnoreChange, file.Metrics, actual, newLogMetric)
}

func newLogMetric() *LogMetric { return &LogMetric{} }
