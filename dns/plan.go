package dns

import (
	"github.com/sinmetalcraft/bizmac/resource"
)

// Plan は yaml と Google Cloud の現状を突き合わせた結果。
type Plan = resource.Plan[*RecordSet]

// BuildPlan は yaml の定義 (file) と Google Cloud の現状 (actual) を比較して Plan を作る。
func BuildPlan(file *File, actual []*RecordSet) (*Plan, error) {
	return resource.BuildPlan(file.Project, "", file.IgnoreChange, file.GetItems(), actual, newRecordSet)
}

func newRecordSet() *RecordSet { return &RecordSet{} }
