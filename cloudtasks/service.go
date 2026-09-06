package cloudtasks

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	tasksapi "cloud.google.com/go/cloudtasks/apiv2beta3"
	cloudtaskspb "cloud.google.com/go/cloudtasks/apiv2beta3/cloudtaskspb"
	"github.com/sinmetalcraft/bizmac/prop"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// leafMaskFields はフィールドマスクを葉のプロパティまで指定するトップレベルフィールド。
// これ以外のフィールドは変更があったらフィールドごと差し替える。
var leafMaskFields = map[string]bool{
	"rate_limits":  true,
	"retry_config": true,
}

// Service は Cloud Tasks API のクライアント。
type Service struct {
	client *tasksapi.Client
	// notes は直近の List で除外したキューの注記。
	notes []string
}

// NewService は Application Default Credentials でクライアントを作る。
func NewService(ctx context.Context) (*Service, error) {
	c, err := tasksapi.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloudtasks: create client: %w", err)
	}
	return &Service{client: c}, nil
}

// Close はクライアントを閉じる。
func (s *Service) Close() error { return s.client.Close() }

// Notes は直近の List で除外したキューの注記を返す。
func (s *Service) Notes() []string { return s.notes }

// List は指定した project/location のキューを名前順で返す。
// PULL キューは管理対象外として除外する。
func (s *Service) List(ctx context.Context, project, location string) ([]*Queue, error) {
	it := s.client.ListQueues(ctx, &cloudtaskspb.ListQueuesRequest{
		Parent: LocationPath(project, location),
	})
	var (
		queues  []*Queue
		skipped []string
	)
	for {
		pb, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cloudtasks: list queues in %s: %w", LocationPath(project, location), err)
		}
		// PULL キューは App Engine 時代の遺物で新規作成もできない。
		// 一覧に含めると yaml に定義が無い = vacuum の削除候補になってしまうため、
		// ここで落として bizmac の管理対象から外す。
		if pb.GetType() == cloudtaskspb.Queue_PULL {
			skipped = append(skipped, path.Base(pb.GetName()))
			continue
		}
		queues = append(queues, QueueFromProto(pb))
	}
	sort.Slice(queues, func(i, j int) bool { return queues[i].Name < queues[j].Name })

	s.notes = nil
	if len(skipped) > 0 {
		sort.Strings(skipped)
		s.notes = append(s.notes, fmt.Sprintf("PULL キュー %d 件は管理対象外のため除外しました (%s)",
			len(skipped), strings.Join(skipped, ", ")))
	}
	return queues, nil
}

// Create はキューを新規作成する。
func (s *Service) Create(ctx context.Context, project, location string, q *Queue) error {
	pb, err := q.ToProto(project, location)
	if err != nil {
		return err
	}
	if _, err := s.client.CreateQueue(ctx, &cloudtaskspb.CreateQueueRequest{
		Parent: LocationPath(project, location),
		Queue:  pb,
	}); err != nil {
		return fmt.Errorf("cloudtasks: create queue %q: %w", q.Name, err)
	}
	return nil
}

// Update は既存キューを更新する。差分のあったプロパティだけをフィールドマスクに入れる。
func (s *Service) Update(ctx context.Context, project, location string, q *Queue, changes []prop.Change) error {
	mask := updateMask(changes)
	if len(mask.GetPaths()) == 0 {
		return nil
	}
	pb, err := q.ToProto(project, location)
	if err != nil {
		return err
	}
	if _, err := s.client.UpdateQueue(ctx, &cloudtaskspb.UpdateQueueRequest{
		Queue:      pb,
		UpdateMask: mask,
	}); err != nil {
		return fmt.Errorf("cloudtasks: update queue %q: %w", q.Name, err)
	}
	return nil
}

// Delete はキューを削除する。
func (s *Service) Delete(ctx context.Context, project, location, name string) error {
	if err := s.client.DeleteQueue(ctx, &cloudtaskspb.DeleteQueueRequest{
		Name: QueuePath(project, location, name),
	}); err != nil {
		return fmt.Errorf("cloudtasks: delete queue %q: %w", name, err)
	}
	return nil
}

// updateMask は差分のあったプロパティから UpdateQueue のフィールドマスクを組み立てる。
// yaml のプロパティ名は proto のフィールド名と同じなので、変換せずに深さだけ丸める。
// Cloud Tasks はキューの全プロパティに既定値を埋めるので、マスクを固定にすると
// yaml に書いていないプロパティまで 0 で上書きしてしまう。
func updateMask(changes []prop.Change) *fieldmaskpb.FieldMask {
	seen := map[string]struct{}{}
	var paths []string
	for _, c := range changes {
		p := maskPath(c.Path)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return &fieldmaskpb.FieldMask{Paths: paths}
}

// maskPath は差分のパスをフィールドマスクのパスへ丸める。
// rate_limits と retry_config は葉のプロパティまで、それ以外はトップレベルまで。
func maskPath(diffPath string) string {
	keys := strings.Split(diffPath, ".")
	switch keys[0] {
	case "", "name", "ignore_change":
		// 名前は変わらないし、ignore_change は Google Cloud 側に無いメタ情報。
		return ""
	}
	if len(keys) > 1 && leafMaskFields[keys[0]] {
		return keys[0] + "." + keys[1]
	}
	return keys[0]
}
