package logmetric

import (
	"context"
	"fmt"
	"sort"

	"github.com/sinmetalcraft/bizmac/prop"
	loggingapi "google.golang.org/api/logging/v2"
)

// Service は Cloud Logging API のクライアント。
type Service struct {
	logging *loggingapi.Service
}

// NewService は Application Default Credentials でクライアントを作る。
func NewService(ctx context.Context) (*Service, error) {
	l, err := loggingapi.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("logmetric: create client: %w", err)
	}
	return &Service{logging: l}, nil
}

// Close は resource.Service の実装。REST クライアントは閉じるものが無い。
func (s *Service) Close() error { return nil }

// List はプロジェクトのログベースの指標を名前順で返す。location は使わない。
// API が返すのはユーザー定義の指標だけで、組み込みの指標は含まれない。
func (s *Service) List(ctx context.Context, project, _ string) ([]*LogMetric, error) {
	var metrics []*LogMetric
	err := s.logging.Projects.Metrics.List(ProjectPath(project)).Context(ctx).Pages(ctx, func(resp *loggingapi.ListLogMetricsResponse) error {
		for _, m := range resp.Metrics {
			metrics = append(metrics, FromAPI(m))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("logmetric: list metrics in %s: %w", project, err)
	}
	sort.Slice(metrics, func(i, k int) bool { return metrics[i].Name < metrics[k].Name })
	return metrics, nil
}

// Create は指標を作る。
func (s *Service) Create(ctx context.Context, project, _ string, m *LogMetric) error {
	api := &loggingapi.LogMetric{}
	m.ApplyTo(api)
	if _, err := s.logging.Projects.Metrics.Create(ProjectPath(project), api).Context(ctx).Do(); err != nil {
		return fmt.Errorf("logmetric: create metric %q: %w", m.Name, err)
	}
	return nil
}

// Update は既存の指標を m のとおりにする。
// API の update は全体を置き換える PUT でフィールドマスクが無いので、現在の指標を
// 読んでから管理しているフィールドだけを上書きして送る。disabled など yaml で
// 扱わないフィールドは今の値のまま残る。
func (s *Service) Update(ctx context.Context, project, _ string, m *LogMetric, _ []prop.Change) error {
	name := MetricPath(project, m.Name)
	current, err := s.logging.Projects.Metrics.Get(name).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("logmetric: get metric %q: %w", m.Name, err)
	}
	m.ApplyTo(current)
	if _, err := s.logging.Projects.Metrics.Update(name, current).Context(ctx).Do(); err != nil {
		return fmt.Errorf("logmetric: update metric %q: %w", m.Name, err)
	}
	return nil
}

// Delete は指標を削除する。
func (s *Service) Delete(ctx context.Context, project, _, name string) error {
	if _, err := s.logging.Projects.Metrics.Delete(MetricPath(project, name)).Context(ctx).Do(); err != nil {
		return fmt.Errorf("logmetric: delete metric %q: %w", name, err)
	}
	return nil
}
