package cloudtasks

import (
	"github.com/sinmetalcraft/bizmac/resource"
)

// Plan は yaml と Google Cloud の現状を突き合わせた結果。
type Plan = resource.Plan[*Queue]

// QueueUpdate は既存キュー 1 件の更新内容。
type QueueUpdate = resource.Update[*Queue]

// BuildPlan は yaml の定義 (file) と Google Cloud の現状 (actual) を比較して Plan を作る。
func BuildPlan(file *File, actual []*Queue) (*Plan, error) {
	return resource.BuildPlan(file.Project, file.Location, file.IgnoreChange, file.Queues, actual, newQueue)
}

func newQueue() *Queue { return &Queue{} }
