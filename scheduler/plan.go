package scheduler

import (
	"github.com/sinmetalcraft/bizmac/resource"
)

// Plan は yaml と Google Cloud の現状を突き合わせた結果。
type Plan = resource.Plan[*Job]

// JobUpdate は既存ジョブ 1 件の更新内容。
type JobUpdate = resource.Update[*Job]

// BuildPlan は yaml の定義 (file) と Google Cloud の現状 (actual) を比較して Plan を作る。
func BuildPlan(file *File, actual []*Job) (*Plan, error) {
	return resource.BuildPlan(file.Project, file.Location, file.IgnoreChange, file.Jobs, actual, newJob)
}

func newJob() *Job { return &Job{} }
