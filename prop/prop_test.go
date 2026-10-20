package prop_test

import (
	"reflect"
	"testing"

	"github.com/sinmetalcraft/bizmac/prop"
)

func TestDiff(t *testing.T) {
	actual := prop.Tree{
		"schedule": "0 3 * * *",
		"http_target": map[string]any{
			"uri":     "https://example.com/a",
			"headers": map[string]any{"User-Agent": "Google-Cloud-Scheduler"},
		},
		"description": "old",
	}
	desired := prop.Tree{
		"schedule": "0 4 * * *",
		"http_target": map[string]any{
			"uri":     "https://example.com/a",
			"headers": map[string]any{"User-Agent": "Google-Cloud-Scheduler"},
		},
		"time_zone": "Asia/Tokyo",
	}

	got := prop.Diff(actual, desired)
	want := []prop.Change{
		{Path: "description", Type: prop.Removed, Old: "old"},
		{Path: "schedule", Type: prop.Modified, Old: "0 3 * * *", New: "0 4 * * *"},
		{Path: "time_zone", Type: prop.Added, New: "Asia/Tokyo"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Diff() = %#v, want %#v", got, want)
	}
}

func TestDiff_noChange(t *testing.T) {
	tree := prop.Tree{"a": map[string]any{"b": []any{1, 2, 3}}}
	if got := prop.Diff(tree, prop.CopyTree(tree)); len(got) != 0 {
		t.Errorf("Diff() = %v, want empty", got)
	}
}

func TestCopyPath(t *testing.T) {
	actual := prop.Tree{
		"http_target": map[string]any{
			"headers": map[string]any{"User-Agent": "Google-Cloud-Scheduler", "X-Keep": "1"},
		},
		"description": "from gcp",
	}

	cases := []struct {
		name    string
		desired prop.Tree
		path    string
		want    prop.Tree
	}{
		{
			name:    "actual の値で上書きする",
			desired: prop.Tree{"description": "from yaml"},
			path:    "description",
			want:    prop.Tree{"description": "from gcp"},
		},
		{
			name: "actual に無いプロパティは desired からも消す",
			desired: prop.Tree{
				"http_target": map[string]any{"headers": map[string]any{"X-Gone": "1"}},
			},
			path: "http_target.headers.X-Gone",
			want: prop.Tree{"http_target": map[string]any{"headers": map[string]any{}}},
		},
		{
			name:    "desired に中間ノードが無い場合は作って埋める",
			desired: prop.Tree{},
			path:    "http_target.headers.User-Agent",
			want: prop.Tree{
				"http_target": map[string]any{"headers": map[string]any{"User-Agent": "Google-Cloud-Scheduler"}},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			prop.CopyPath(tt.desired, actual, tt.path)
			if !reflect.DeepEqual(tt.desired, tt.want) {
				t.Errorf("CopyPath(%q) = %#v, want %#v", tt.path, tt.desired, tt.want)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	tree := prop.Tree{
		"jobs": []any{
			map[string]any{"name": "a", "ignore_change": []any{"x"}},
			map[string]any{"name": "b", "ignore_change": []any{"y"}},
		},
	}
	// 配列の途中にあるプロパティは全要素へ適用される。
	prop.Delete(tree, "jobs.ignore_change")
	want := prop.Tree{
		"jobs": []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}},
	}
	if !reflect.DeepEqual(tree, want) {
		t.Errorf("Delete() = %#v, want %#v", tree, want)
	}
}

func TestChangeString(t *testing.T) {
	cases := []struct {
		change prop.Change
		want   string
	}{
		{prop.Change{Path: "a.b", Type: prop.Added, New: 3}, "+ a.b: 3"},
		{prop.Change{Path: "a.b", Type: prop.Removed, Old: "x"}, `- a.b: "x"`},
		{prop.Change{Path: "a.b", Type: prop.Modified, Old: "x", New: "y"}, `~ a.b: "x" => "y"`},
	}
	for _, tt := range cases {
		if got := tt.change.String(); got != tt.want {
			t.Errorf("Change.String() = %q, want %q", got, tt.want)
		}
	}
}

func TestCopyPathIfAbsent(t *testing.T) {
	actual := map[string]any{
		"rate_limits": map[string]any{
			"max_dispatches_per_second": 500,
			"max_concurrent_dispatches": 1000,
		},
		"task_ttl": "744h0m0s",
	}

	// dst に書いてある値は残り、書いていない値だけ src から入る。
	desired := map[string]any{
		"rate_limits": map[string]any{"max_dispatches_per_second": 10},
	}
	for _, path := range []string{
		"rate_limits.max_dispatches_per_second",
		"rate_limits.max_concurrent_dispatches",
		"task_ttl",
	} {
		prop.CopyPathIfAbsent(desired, actual, path)
	}

	rl, _ := desired["rate_limits"].(map[string]any)
	if rl["max_dispatches_per_second"] != 10 {
		t.Errorf("max_dispatches_per_second = %v, want 10 (yaml の値が残る)", rl["max_dispatches_per_second"])
	}
	if rl["max_concurrent_dispatches"] != 1000 {
		t.Errorf("max_concurrent_dispatches = %v, want 1000 (現状の値が入る)", rl["max_concurrent_dispatches"])
	}
	if desired["task_ttl"] != "744h0m0s" {
		t.Errorf("task_ttl = %v, want 744h0m0s (現状の値が入る)", desired["task_ttl"])
	}
}

func TestCopyPathIfAbsent_missingInBoth(t *testing.T) {
	// どちらにも無いパスを写しても、中間ノードを作らない。
	desired := map[string]any{}
	prop.CopyPathIfAbsent(desired, map[string]any{}, "retry_config.max_attempts")
	if len(desired) != 0 {
		t.Errorf("desired = %v, want 空", desired)
	}
}

func changeLines(changes []prop.Change) []string {
	lines := make([]string, 0, len(changes))
	for _, c := range changes {
		lines = append(lines, c.String())
	}
	return lines
}

func TestDiffSets(t *testing.T) {
	// 集合は並び順を無視して要素単位で比べる。map のキーに "." があっても扱える。
	actual := prop.Tree{
		"roles": []any{"roles/a", "roles/b", "roles/d"},
		"policy": map[string]any{
			"roles/iam.x": []any{"user:a@example.com"},
			"roles/iam.y": []any{"user:b@example.com"},
		},
		"order": []any{"x", "y"},
	}
	desired := prop.Tree{
		"roles": []any{"roles/d", "roles/c", "roles/a"},
		"policy": map[string]any{
			"roles/iam.x": []any{"user:a@example.com", "user:c@example.com"},
			"roles/iam.z": []any{"user:d@example.com"},
		},
		"order": []any{"y", "x"},
	}

	got := changeLines(prop.DiffSets(actual, desired, []string{"roles", "policy.*"}))
	want := []string{
		// 集合でない配列は従来どおり位置で比べる。
		`~ order[0]: "x" => "y"`,
		`~ order[1]: "y" => "x"`,
		`+ policy.roles/iam.x: "user:c@example.com"`,
		`- policy.roles/iam.y: "user:b@example.com"`,
		`+ policy.roles/iam.z: "user:d@example.com"`,
		`- roles: "roles/b"`,
		`+ roles: "roles/c"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiffSets() =\n%q\nwant\n%q", got, want)
	}
}

func TestDiffSets_wholeSetAddedOrRemoved(t *testing.T) {
	// 集合を含むプロパティが丸ごと増減した場合も要素単位になる。
	actual := prop.Tree{"policy": map[string]any{"r": []any{"user:a"}}}
	desired := prop.Tree{"roles": []any{"roles/a"}}

	got := changeLines(prop.DiffSets(actual, desired, []string{"roles", "policy.*"}))
	want := []string{`- policy.r: "user:a"`, `+ roles: "roles/a"`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiffSets() = %q, want %q", got, want)
	}
	for _, c := range prop.DiffSets(actual, desired, []string{"roles", "policy.*"}) {
		if !c.IsElement() {
			t.Errorf("%s: IsElement() = false, want true", c)
		}
	}
}

func TestApplyElement(t *testing.T) {
	sets := []string{"roles", "policy.*"}
	actual := prop.Tree{
		"roles":  []any{"roles/a", "roles/b"},
		"policy": map[string]any{"roles/iam.x": []any{"user:a"}},
	}
	desired := prop.Tree{"roles": []any{"roles/a", "roles/c"}}
	changes := prop.DiffSets(actual, desired, sets)

	// 差分をすべて適用すると desired と同じ差分なしの状態になる。
	applied := prop.CopyTree(actual)
	for _, c := range changes {
		prop.ApplyElement(applied, c)
	}
	if d := prop.DiffSets(applied, desired, sets); len(d) != 0 {
		t.Errorf("ApplyElement 後も差分があります: %v (tree: %v)", d, applied)
	}
	// 空になった集合と map は取り除かれる。
	if _, ok := applied["policy"]; ok {
		t.Errorf("policy = %v, want 取り除かれている", applied["policy"])
	}

	// 適用したものを戻すと actual に戻る。
	for _, c := range changes {
		prop.RevertElement(applied, c)
	}
	if d := prop.DiffSets(actual, applied, sets); len(d) != 0 {
		t.Errorf("RevertElement 後に差分があります: %v", d)
	}
}
