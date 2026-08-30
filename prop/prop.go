// Package prop はリソース定義を汎用のプロパティツリー(map/slice/scalar)として扱い、
// ignore_change の適用・差分抽出・マージを行う。
// リソース種別に依存しないので、Cloud Scheduler 以外を足すときもそのまま使える。
package prop

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Tree はプロパティツリーのルート。
type Tree = map[string]any

// Normalize は yaml タグの付いた構造体をプロパティツリーに変換する。
func Normalize(v any) (Tree, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("prop: marshal: %w", err)
	}
	t := Tree{}
	if err := yaml.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("prop: unmarshal: %w", err)
	}
	if t == nil {
		t = Tree{}
	}
	return t, nil
}

// Decode はプロパティツリーを yaml タグの付いた構造体へ戻す。
func Decode(t Tree, v any) error {
	b, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("prop: marshal tree: %w", err)
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("prop: decode tree: %w", err)
	}
	return nil
}

// Copy はプロパティツリーのディープコピーを返す。
func Copy(v any) any {
	switch n := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(n))
		for k, e := range n {
			m[k] = Copy(e)
		}
		return m
	case []any:
		s := make([]any, len(n))
		for i, e := range n {
			s[i] = Copy(e)
		}
		return s
	default:
		return v
	}
}

// CopyTree は Copy の Tree 版。
func CopyTree(t Tree) Tree {
	c, _ := Copy(map[string]any(t)).(map[string]any)
	return c
}

// Delete は path で指定されたプロパティを取り除く。
// path はドット区切り (例: "http_target.headers.User-Agent")。
// 途中に配列があった場合は、その全要素に対して適用する。
func Delete(node any, path string) {
	deleteKeys(node, splitPath(path))
}

func deleteKeys(node any, keys []string) {
	if len(keys) == 0 {
		return
	}
	switch n := node.(type) {
	case map[string]any:
		if len(keys) == 1 {
			delete(n, keys[0])
			return
		}
		if child, ok := n[keys[0]]; ok {
			deleteKeys(child, keys[1:])
		}
	case []any:
		for _, e := range n {
			deleteKeys(e, keys)
		}
	}
}

// CopyPath は src の path の値を dst の同じ path に上書きする。
// src に無ければ dst からも取り除く。ignore_change の適用に使う。
func CopyPath(dst, src any, path string) {
	copyKeys(dst, src, splitPath(path))
}

func copyKeys(dst, src any, keys []string) {
	if len(keys) == 0 {
		return
	}
	switch d := dst.(type) {
	case map[string]any:
		s, _ := src.(map[string]any)
		k := keys[0]
		if len(keys) == 1 {
			if s != nil {
				if v, ok := s[k]; ok {
					d[k] = Copy(v)
					return
				}
			}
			delete(d, k)
			return
		}
		var sc any
		if s != nil {
			sc = s[k]
		}
		dc, ok := d[k]
		if !ok {
			// dst 側に中間ノードが無い場合は src 側の形に合わせて作る。
			if sc == nil {
				return
			}
			dc = map[string]any{}
			d[k] = dc
		}
		copyKeys(dc, sc, keys[1:])
	case []any:
		s, _ := src.([]any)
		for i, e := range d {
			var se any
			if i < len(s) {
				se = s[i]
			}
			copyKeys(e, se, keys)
		}
	}
}

// ChangeType は差分の種類。
type ChangeType string

const (
	// Added は desired にだけ存在するプロパティ。
	Added ChangeType = "added"
	// Removed は actual にだけ存在するプロパティ。
	Removed ChangeType = "removed"
	// Modified は両方に存在して値が異なるプロパティ。
	Modified ChangeType = "modified"
)

// Change は 1 プロパティ分の差分。
type Change struct {
	Path string
	Type ChangeType
	Old  any // actual (Google Cloud) 側の値
	New  any // desired (yaml) 側の値
}

// String は "+ retry_config.retry_count: 3" のような 1 行表現を返す。
func (c Change) String() string {
	switch c.Type {
	case Added:
		return fmt.Sprintf("+ %s: %s", c.Path, FormatValue(c.New))
	case Removed:
		return fmt.Sprintf("- %s: %s", c.Path, FormatValue(c.Old))
	default:
		return fmt.Sprintf("~ %s: %s => %s", c.Path, FormatValue(c.Old), FormatValue(c.New))
	}
}

// Diff は actual を desired に合わせるために必要な変更を返す。パス順にソートされる。
func Diff(actual, desired any) []Change {
	var out []Change
	diff("", actual, desired, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func diff(path string, actual, desired any, out *[]Change) {
	am, aIsMap := actual.(map[string]any)
	dm, dIsMap := desired.(map[string]any)
	if aIsMap && dIsMap {
		for _, k := range unionKeys(am, dm) {
			av, aok := am[k]
			dv, dok := dm[k]
			p := joinPath(path, k)
			switch {
			case aok && dok:
				diff(p, av, dv, out)
			case dok:
				*out = append(*out, Change{Path: p, Type: Added, New: dv})
			default:
				*out = append(*out, Change{Path: p, Type: Removed, Old: av})
			}
		}
		return
	}

	as, aIsSlice := actual.([]any)
	ds, dIsSlice := desired.([]any)
	if aIsSlice && dIsSlice {
		n := len(as)
		if len(ds) > n {
			n = len(ds)
		}
		for i := 0; i < n; i++ {
			p := fmt.Sprintf("%s[%d]", path, i)
			switch {
			case i < len(as) && i < len(ds):
				diff(p, as[i], ds[i], out)
			case i < len(ds):
				*out = append(*out, Change{Path: p, Type: Added, New: ds[i]})
			default:
				*out = append(*out, Change{Path: p, Type: Removed, Old: as[i]})
			}
		}
		return
	}

	if !reflect.DeepEqual(actual, desired) {
		*out = append(*out, Change{Path: path, Type: Modified, Old: actual, New: desired})
	}
}

func unionKeys(a, b map[string]any) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	keys := make([]string, 0, len(a)+len(b))
	for _, m := range []map[string]any{a, b} {
		for k := range m {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// FormatValue は差分表示用に値を 1 行へ整形する。
func FormatValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "(none)"
	case string:
		return fmt.Sprintf("%q", t)
	case map[string]any, []any:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func splitPath(path string) []string {
	return strings.Split(path, ".")
}

func joinPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}
