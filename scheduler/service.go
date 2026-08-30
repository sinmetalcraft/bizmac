package scheduler

import (
	"context"
	"fmt"
	"sort"

	cloudscheduler "cloud.google.com/go/scheduler/apiv1"
	schedulerpb "cloud.google.com/go/scheduler/apiv1/schedulerpb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// updateMaskPaths は update で上書きする Job のフィールド。
// ターゲットの oneof は種別ごとに異なるので実行時に足す。
var updateMaskPaths = []string{
	"description",
	"schedule",
	"time_zone",
	"attempt_deadline",
	"retry_config",
}

// Service は Cloud Scheduler API のクライアント。
type Service struct {
	client *cloudscheduler.CloudSchedulerClient
}

// NewService は Application Default Credentials でクライアントを作る。
func NewService(ctx context.Context) (*Service, error) {
	c, err := cloudscheduler.NewCloudSchedulerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("scheduler: create client: %w", err)
	}
	return &Service{client: c}, nil
}

// Close はクライアントを閉じる。
func (s *Service) Close() error { return s.client.Close() }

// List は指定した project/location のジョブを名前順で返す。
func (s *Service) List(ctx context.Context, project, location string) ([]*Job, error) {
	it := s.client.ListJobs(ctx, &schedulerpb.ListJobsRequest{
		Parent: LocationPath(project, location),
	})
	var jobs []*Job
	for {
		pb, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("scheduler: list jobs in %s: %w", LocationPath(project, location), err)
		}
		jobs = append(jobs, JobFromProto(pb))
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
	return jobs, nil
}

// Create はジョブを新規作成する。
func (s *Service) Create(ctx context.Context, project, location string, j *Job) error {
	pb, err := j.ToProto(project, location)
	if err != nil {
		return err
	}
	if _, err := s.client.CreateJob(ctx, &schedulerpb.CreateJobRequest{
		Parent: LocationPath(project, location),
		Job:    pb,
	}); err != nil {
		return fmt.Errorf("scheduler: create job %q: %w", j.Name, err)
	}
	return nil
}

// Update は既存ジョブを更新する。
func (s *Service) Update(ctx context.Context, project, location string, j *Job) error {
	pb, err := j.ToProto(project, location)
	if err != nil {
		return err
	}
	kind := j.TargetKind()
	if kind == "" {
		return fmt.Errorf("scheduler: job %q has no target", j.Name)
	}
	mask := &fieldmaskpb.FieldMask{Paths: append(append([]string{}, updateMaskPaths...), kind)}
	if _, err := s.client.UpdateJob(ctx, &schedulerpb.UpdateJobRequest{
		Job:        pb,
		UpdateMask: mask,
	}); err != nil {
		return fmt.Errorf("scheduler: update job %q: %w", j.Name, err)
	}
	return nil
}

// Delete はジョブを削除する。
func (s *Service) Delete(ctx context.Context, project, location, name string) error {
	if err := s.client.DeleteJob(ctx, &schedulerpb.DeleteJobRequest{
		Name: JobPath(project, location, name),
	}); err != nil {
		return fmt.Errorf("scheduler: delete job %q: %w", name, err)
	}
	return nil
}
