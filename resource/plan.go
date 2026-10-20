package resource

import (
	"fmt"

	"github.com/sinmetalcraft/bizmac/prop"
)

// Plan は yaml と Google Cloud の現状を突き合わせた結果。
type Plan[T Item] struct {
	Project  string
	Location string
	// Notes は管理対象外として除外したリソースなどの注記。
	Notes []string
	// Create は yaml にあって Google Cloud に無いリソース。
	Create []T
	// Update は両方にあって差分のあるリソース。
	Update []*Update[T]
	// Delete は Google Cloud にあって yaml に無いリソース。vacuum の対象。
	Delete []T
	// Revoke は yaml から外れた付与 (Grants) を持つリソース。vacuum の対象。
	Revoke []*Revoke[T]
	// NoChange は差分の無いリソース。
	NoChange []T
}

// HasChange は update で適用すべき変更があるかを返す。
func (p *Plan[T]) HasChange() bool {
	return len(p.Create) > 0 || len(p.Update) > 0
}

// HasVacuum は vacuum で適用すべき変更があるかを返す。
func (p *Plan[T]) HasVacuum() bool {
	return len(p.Delete) > 0 || len(p.Revoke) > 0
}

// Update は既存リソース 1 件の更新内容。
type Update[T Item] struct {
	// Name はリソース ID。
	Name string
	// Actual は Google Cloud 側の現在の定義。
	Actual T
	// Desired は ignore_change を適用済みの、実際に送信する定義。
	// 無視対象のプロパティには Actual の値が入っている。
	Desired T
	// Changes は Actual を Desired にするための差分。
	Changes []prop.Change
	// RecreateRequired は RecreateKey が変わっていて update では適用できない場合に true。
	// この場合は一度削除して作り直す必要がある。
	RecreateRequired bool
}

// Revoke は既存リソース 1 件の、yaml から外れた付与の取り消し内容。
type Revoke[T Item] struct {
	// Name はリソース ID。
	Name string
	// Desired は Google Cloud 側の現在の定義から、取り消す要素だけを除いたもの。
	// 付与の追加や他のプロパティの変更は含まない。
	Desired T
	// Changes は取り消す要素。すべて IsElement が true の Removed。
	Changes []prop.Change
}

// BuildPlan は yaml の定義 (want) と Google Cloud の現状 (actual) を比較して Plan を作る。
// ignore はファイル全体に適用される ignore_change、newItem は空のリソースを作る関数。
func BuildPlan[T Item](project, location string, ignore []string, want, actual []T, newItem func() T) (*Plan[T], error) {
	actualByName := make(map[string]T, len(actual))
	for _, a := range actual {
		actualByName[a.ItemName()] = a
	}

	p := &Plan[T]{Project: project, Location: location}
	desiredNames := make(map[string]struct{}, len(want))

	for _, w := range want {
		desiredNames[w.ItemName()] = struct{}{}

		got, ok := actualByName[w.ItemName()]
		if !ok {
			c, err := cloneItem(w, newItem)
			if err != nil {
				return nil, err
			}
			c.SetItemIgnoreChange(nil)
			p.Create = append(p.Create, c)
			continue
		}

		u, r, err := buildUpdate(ignore, w, got, newItem)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", w.ItemName(), err)
		}
		if len(u.Changes) == 0 && r == nil {
			p.NoChange = append(p.NoChange, got)
			continue
		}
		if len(u.Changes) > 0 {
			p.Update = append(p.Update, u)
		}
		if r != nil {
			p.Revoke = append(p.Revoke, r)
		}
	}

	for _, a := range actual {
		if _, ok := desiredNames[a.ItemName()]; !ok {
			p.Delete = append(p.Delete, a)
		}
	}

	return p, nil
}

// buildUpdate は 1 件分の更新内容を作る。付与の取り消しがある場合は Revoke も返す。
func buildUpdate[T Item](ignore []string, want, got T, newItem func() T) (*Update[T], *Revoke[T], error) {
	// body/body_json のような同義表現をそろえてから比較する。
	wantCanon, err := cloneItem(want, newItem)
	if err != nil {
		return nil, nil, err
	}
	gotCanon, err := cloneItem(got, newItem)
	if err != nil {
		return nil, nil, err
	}

	desiredTree, err := prop.Normalize(wantCanon)
	if err != nil {
		return nil, nil, err
	}
	actualTree, err := prop.Normalize(gotCanon)
	if err != nil {
		return nil, nil, err
	}
	// ignore_change は Google Cloud 側に存在しないメタ情報なので比較対象から外す。
	prop.Delete(desiredTree, "ignore_change")

	// 無視対象のプロパティには Google Cloud 側の値を持ってくる。
	// これで差分にも出ず、update で上書きもされなくなる。
	merged := prop.CopyTree(desiredTree)
	for _, path := range ignorePaths(ignore, want) {
		prop.CopyPath(merged, actualTree, path)
	}

	// 未設定へ戻せないプロパティは、yaml に書かなければ現状の値をそのまま使う。
	if sd, ok := any(want).(ServerDefaults); ok {
		for _, path := range sd.ServerDefaultPaths() {
			prop.CopyPathIfAbsent(merged, actualTree, path)
		}
	}

	var grantPaths []string
	if g, ok := any(want).(Grants); ok {
		grantPaths = g.GrantPaths()
	}
	changes := prop.DiffSets(actualTree, merged, grantPaths)

	// 付与の取り消しは update から外して vacuum に回す。
	// update では Google Cloud 側の要素を残し、vacuum では現状から取り消す要素だけを除く。
	var keep, revokes []prop.Change
	for _, c := range changes {
		if c.IsElement() && c.Type == prop.Removed {
			revokes = append(revokes, c)
			continue
		}
		keep = append(keep, c)
	}
	var revoke *Revoke[T]
	if len(revokes) > 0 {
		revoked := prop.CopyTree(actualTree)
		for _, c := range revokes {
			prop.RevertElement(merged, c)
			prop.ApplyElement(revoked, c)
		}
		r := newItem()
		if err := prop.Decode(revoked, r); err != nil {
			return nil, nil, err
		}
		if err := r.Canonicalize(); err != nil {
			return nil, nil, err
		}
		revoke = &Revoke[T]{Name: want.ItemName(), Desired: r, Changes: revokes}
	}

	desired := newItem()
	if err := prop.Decode(merged, desired); err != nil {
		return nil, nil, err
	}
	if len(revokes) > 0 {
		// 足し戻した要素の並びをそろえる。
		if err := desired.Canonicalize(); err != nil {
			return nil, nil, err
		}
	}

	return &Update[T]{
		Name:             want.ItemName(),
		Actual:           gotCanon,
		Desired:          desired,
		Changes:          keep,
		RecreateRequired: desired.RecreateKey() != gotCanon.RecreateKey(),
	}, revoke, nil
}

// ignorePaths はファイル全体とリソース個別の ignore_change を結合して返す。
func ignorePaths[T Item](fileIgnore []string, item T) []string {
	own := item.ItemIgnoreChange()
	paths := make([]string, 0, len(fileIgnore)+len(own))
	paths = append(paths, fileIgnore...)
	paths = append(paths, own...)
	return paths
}

// cloneItem はリソースをコピーし、body/body_json などの表現を正規化して返す。
func cloneItem[T Item](item T, newItem func() T) (T, error) {
	var zero T
	t, err := prop.Normalize(item)
	if err != nil {
		return zero, err
	}
	c := newItem()
	if err := prop.Decode(t, c); err != nil {
		return zero, err
	}
	if err := c.Canonicalize(); err != nil {
		return zero, fmt.Errorf("%q: %w", item.ItemName(), err)
	}
	return c, nil
}
