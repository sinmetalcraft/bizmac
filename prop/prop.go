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

// CopyPathIfAbsent は dst に path の値が無い場合だけ src から写す。
// Google Cloud が既定値を必ず埋めるうえに未設定へ戻せないプロパティを、
// 「yaml に書かなければ現状の値をそのまま使う」と解釈するために使う。
func CopyPathIfAbsent(dst, src any, path string) {
	keys := splitPath(path)
	if hasKeys(dst, keys) {
		return
	}
	copyKeys(dst, src, keys)
}

func hasKeys(node any, keys []string) bool {
	if len(keys) == 0 {
		return false
	}
	switch n := node.(type) {
	case map[string]any:
		v, ok := n[keys[0]]
		if !ok {
			return false
		}
		if len(keys) == 1 {
			return true
		}
		return hasKeys(v, keys[1:])
	case []any:
		// 配列は 1 要素でも持っていれば「ある」とみなす。
		for _, e := range n {
			if hasKeys(e, keys) {
				return true
			}
		}
	}
	return false
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

	// keys は Path をキーごとに分けたもの。集合の要素の差分でだけ使う。
	// map のキーに "." を含むことがある (IAM のロール名など) ので Path からは戻せない。
	keys []string
	// element は集合 (順序に意味の無い配列) の要素単位の差分であることを示す。
	element bool
}

// IsElement は集合の要素単位の差分かを返す。
// Added なら New が、Removed なら Old が追加・削除された 1 要素になる。
func (c Change) IsElement() bool { return c.element }

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
	return DiffSets(actual, desired, nil)
}

// DiffSets は Diff と同じだが、sets に一致するパスの配列を集合として扱い、
// 並び順を無視して要素単位の差分 (IsElement が true) を返す。
// sets はドット区切りで、"*" は map の任意のキー 1 つに一致する (例: "iam_policy.*")。
func DiffSets(actual, desired any, sets []string) []Change {
	patterns := make([][]string, 0, len(sets))
	for _, s := range sets {
		patterns = append(patterns, splitPath(s))
	}
	var out []Change
	d := differ{sets: patterns, out: &out}
	d.diff("", nil, actual, desired)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

type differ struct {
	sets [][]string
	out  *[]Change
}

func (d differ) diff(path string, keys []string, actual, desired any) {
	am, aIsMap := actual.(map[string]any)
	dm, dIsMap := desired.(map[string]any)
	if aIsMap && dIsMap {
		for _, k := range unionKeys(am, dm) {
			av, aok := am[k]
			dv, dok := dm[k]
			p := joinPath(path, k)
			ks := append(append([]string{}, keys...), k)
			switch {
			case aok && dok:
				d.diff(p, ks, av, dv)
			case dok:
				if empty, ok := d.emptyCounterpart(ks, dv); ok {
					d.diff(p, ks, empty, dv)
					continue
				}
				*d.out = append(*d.out, Change{Path: p, Type: Added, New: dv})
			default:
				if empty, ok := d.emptyCounterpart(ks, av); ok {
					d.diff(p, ks, av, empty)
					continue
				}
				*d.out = append(*d.out, Change{Path: p, Type: Removed, Old: av})
			}
		}
		return
	}

	as, aIsSlice := actual.([]any)
	ds, dIsSlice := desired.([]any)
	if aIsSlice && dIsSlice {
		if d.isSet(keys) {
			d.diffSet(path, keys, as, ds)
			return
		}
		n := len(as)
		if len(ds) > n {
			n = len(ds)
		}
		for i := 0; i < n; i++ {
			p := fmt.Sprintf("%s[%d]", path, i)
			switch {
			case i < len(as) && i < len(ds):
				d.diff(p, nil, as[i], ds[i])
			case i < len(ds):
				*d.out = append(*d.out, Change{Path: p, Type: Added, New: ds[i]})
			default:
				*d.out = append(*d.out, Change{Path: p, Type: Removed, Old: as[i]})
			}
		}
		return
	}

	if !reflect.DeepEqual(actual, desired) {
		*d.out = append(*d.out, Change{Path: path, Type: Modified, Old: actual, New: desired})
	}
}

// diffSet は集合どうしの差分を要素単位で出す。取り除く要素、足す要素の順。
func (d differ) diffSet(path string, keys []string, actual, desired []any) {
	for _, e := range actual {
		if !containsValue(desired, e) {
			*d.out = append(*d.out, Change{Path: path, Type: Removed, Old: e, keys: keys, element: true})
		}
	}
	for _, e := range desired {
		if !containsValue(actual, e) {
			*d.out = append(*d.out, Change{Path: path, Type: Added, New: e, keys: keys, element: true})
		}
	}
}

// emptyCounterpart は片側にしか無いプロパティの下に集合がある場合、もう片側の
// 代わりに使う空の値を返す。これで集合が丸ごと増減した場合も要素単位の差分になる。
func (d differ) emptyCounterpart(keys []string, v any) (any, bool) {
	switch v.(type) {
	case []any:
		if d.isSet(keys) {
			return []any{}, true
		}
	case map[string]any:
		if d.hasSetBelow(keys) {
			return map[string]any{}, true
		}
	}
	return nil, false
}

func (d differ) isSet(keys []string) bool {
	if keys == nil {
		return false
	}
	for _, p := range d.sets {
		if len(p) == len(keys) && matchKeys(p, keys) {
			return true
		}
	}
	return false
}

func (d differ) hasSetBelow(keys []string) bool {
	for _, p := range d.sets {
		if len(p) > len(keys) && matchKeys(p[:len(keys)], keys) {
			return true
		}
	}
	return false
}

func matchKeys(pattern, keys []string) bool {
	for i, p := range pattern {
		if p != "*" && p != keys[i] {
			return false
		}
	}
	return true
}

func containsValue(s []any, v any) bool {
	for _, e := range s {
		if reflect.DeepEqual(e, v) {
			return true
		}
	}
	return false
}

// ApplyElement は集合の要素単位の差分 c を t に適用する。
// Added なら要素を足し、Removed なら要素を取り除く。IsElement でない差分は無視する。
func ApplyElement(t Tree, c Change) {
	if !c.element {
		return
	}
	switch c.Type {
	case Added:
		insertElement(t, c.keys, c.New)
	case Removed:
		removeElement(t, c.keys, c.Old)
	}
}

// RevertElement は ApplyElement の逆を行う。
// Added なら要素を取り除き、Removed なら要素を足し戻す。
func RevertElement(t Tree, c Change) {
	if !c.element {
		return
	}
	switch c.Type {
	case Added:
		removeElement(t, c.keys, c.New)
	case Removed:
		insertElement(t, c.keys, c.Old)
	}
}

func insertElement(node map[string]any, keys []string, v any) {
	k := keys[0]
	if len(keys) == 1 {
		s, _ := node[k].([]any)
		if !containsValue(s, v) {
			node[k] = append(s, Copy(v))
		}
		return
	}
	child, ok := node[k].(map[string]any)
	if !ok {
		child = map[string]any{}
		node[k] = child
	}
	insertElement(child, keys[1:], v)
}

// removeElement は要素を取り除き、空になった配列や map もあわせて取り除く。
// yaml は omitempty で空の値を書かないので、そちらと形をそろえるため。
func removeElement(node map[string]any, keys []string, v any) {
	k := keys[0]
	if len(keys) == 1 {
		s, ok := node[k].([]any)
		if !ok {
			return
		}
		out := make([]any, 0, len(s))
		for _, e := range s {
			if !reflect.DeepEqual(e, v) {
				out = append(out, e)
			}
		}
		if len(out) == 0 {
			delete(node, k)
		} else {
			node[k] = out
		}
		return
	}
	child, ok := node[k].(map[string]any)
	if !ok {
		return
	}
	removeElement(child, keys[1:], v)
	if len(child) == 0 {
		delete(node, k)
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
