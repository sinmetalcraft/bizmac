package scheduler

import (
	"fmt"

	"github.com/sinmetalcraft/bizmac/prop"
)

// Plan は yaml と Google Cloud の現状を突き合わせた結果。
type Plan struct {
	Project  string
	Location string
	// Create は yaml にあって Google Cloud に無いジョブ。
	Create []*Job
	// Update は両方にあって差分のあるジョブ。
	Update []*JobUpdate
	// Delete は Google Cloud にあって yaml に無いジョブ。vacuum の対象。
	Delete []*Job
	// NoChange は差分の無いジョブ。
	NoChange []*Job
}

// HasChange は update で適用すべき変更があるかを返す。
func (p *Plan) HasChange() bool {
	return len(p.Create) > 0 || len(p.Update) > 0
}

// JobUpdate は既存ジョブ 1 件の更新内容。
type JobUpdate struct {
	// Name はジョブ ID。
	Name string
	// Actual は Google Cloud 側の現在の定義。
	Actual *Job
	// Desired は ignore_change を適用済みの、実際に送信する定義。
	// 無視対象のプロパティには Actual の値が入っている。
	Desired *Job
	// Changes は Actual を Desired にするための差分。
	Changes []prop.Change
	// TargetKindChanged はターゲット種別そのものが変わっている場合に true。
	// この場合 update では適用できず、削除して作り直す必要がある。
	TargetKindChanged bool
}

// BuildPlan は yaml の定義 (file) と Google Cloud の現状 (actual) を比較して Plan を作る。
func BuildPlan(file *File, actual []*Job) (*Plan, error) {
	actualByName := make(map[string]*Job, len(actual))
	for _, a := range actual {
		actualByName[a.Name] = a
	}

	p := &Plan{Project: file.Project, Location: file.Location}
	desiredNames := make(map[string]struct{}, len(file.Jobs))

	for _, want := range file.Jobs {
		desiredNames[want.Name] = struct{}{}

		got, ok := actualByName[want.Name]
		if !ok {
			c, err := cloneJob(want)
			if err != nil {
				return nil, err
			}
			c.IgnoreChange = nil
			p.Create = append(p.Create, c)
			continue
		}

		u, err := buildJobUpdate(file, want, got)
		if err != nil {
			return nil, fmt.Errorf("job %q: %w", want.Name, err)
		}
		if len(u.Changes) == 0 {
			p.NoChange = append(p.NoChange, got)
			continue
		}
		p.Update = append(p.Update, u)
	}

	for _, a := range actual {
		if _, ok := desiredNames[a.Name]; !ok {
			p.Delete = append(p.Delete, a)
		}
	}

	return p, nil
}

func buildJobUpdate(file *File, want, got *Job) (*JobUpdate, error) {
	// body/body_json のような同義表現をそろえてから比較する。
	wantCanon, err := cloneJob(want)
	if err != nil {
		return nil, err
	}
	gotCanon, err := cloneJob(got)
	if err != nil {
		return nil, err
	}

	desiredTree, err := prop.Normalize(wantCanon)
	if err != nil {
		return nil, err
	}
	actualTree, err := prop.Normalize(gotCanon)
	if err != nil {
		return nil, err
	}
	// ignore_change は Google Cloud 側に存在しないメタ情報なので比較対象から外す。
	prop.Delete(desiredTree, "ignore_change")

	// 無視対象のプロパティには Google Cloud 側の値を持ってくる。
	// これで差分にも出ず、update で上書きもされなくなる。
	merged := prop.CopyTree(desiredTree)
	for _, path := range ignorePaths(file, want) {
		prop.CopyPath(merged, actualTree, path)
	}

	changes := prop.Diff(actualTree, merged)

	desired := &Job{}
	if err := prop.Decode(merged, desired); err != nil {
		return nil, err
	}

	return &JobUpdate{
		Name:              want.Name,
		Actual:            gotCanon,
		Desired:           desired,
		Changes:           changes,
		TargetKindChanged: desired.TargetKind() != gotCanon.TargetKind(),
	}, nil
}

// ignorePaths はファイル全体とジョブ個別の ignore_change を結合して返す。
func ignorePaths(file *File, j *Job) []string {
	paths := make([]string, 0, len(file.IgnoreChange)+len(j.IgnoreChange))
	paths = append(paths, file.IgnoreChange...)
	paths = append(paths, j.IgnoreChange...)
	return paths
}

// cloneJob はジョブをコピーし、body/body_json などの表現を正規化して返す。
func cloneJob(j *Job) (*Job, error) {
	t, err := prop.Normalize(j)
	if err != nil {
		return nil, err
	}
	c := &Job{}
	if err := prop.Decode(t, c); err != nil {
		return nil, err
	}
	if err := c.canonicalizePayloads(); err != nil {
		return nil, fmt.Errorf("job %q: %w", j.Name, err)
	}
	return c, nil
}
