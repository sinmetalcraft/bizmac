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
	// NoChange は差分の無いリソース。
	NoChange []T
}

// HasChange は update で適用すべき変更があるかを返す。
func (p *Plan[T]) HasChange() bool {
	return len(p.Create) > 0 || len(p.Update) > 0
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

		u, err := buildUpdate(ignore, w, got, newItem)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", w.ItemName(), err)
		}
		if len(u.Changes) == 0 {
			p.NoChange = append(p.NoChange, got)
			continue
		}
		p.Update = append(p.Update, u)
	}

	for _, a := range actual {
		if _, ok := desiredNames[a.ItemName()]; !ok {
			p.Delete = append(p.Delete, a)
		}
	}

	return p, nil
}

func buildUpdate[T Item](ignore []string, want, got T, newItem func() T) (*Update[T], error) {
	// body/body_json のような同義表現をそろえてから比較する。
	wantCanon, err := cloneItem(want, newItem)
	if err != nil {
		return nil, err
	}
	gotCanon, err := cloneItem(got, newItem)
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
	for _, path := range ignorePaths(ignore, want) {
		prop.CopyPath(merged, actualTree, path)
	}

	// 未設定へ戻せないプロパティは、yaml に書かなければ現状の値をそのまま使う。
	if sd, ok := any(want).(ServerDefaults); ok {
		for _, path := range sd.ServerDefaultPaths() {
			prop.CopyPathIfAbsent(merged, actualTree, path)
		}
	}

	changes := prop.Diff(actualTree, merged)

	desired := newItem()
	if err := prop.Decode(merged, desired); err != nil {
		return nil, err
	}

	return &Update[T]{
		Name:             want.ItemName(),
		Actual:           gotCanon,
		Desired:          desired,
		Changes:          changes,
		RecreateRequired: desired.RecreateKey() != gotCanon.RecreateKey(),
	}, nil
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
