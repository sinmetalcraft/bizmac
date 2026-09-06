package cloudtasks_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sinmetalcraft/bizmac/cloudtasks"
)

func queue(name string, dispatches float64) *cloudtasks.Queue {
	return &cloudtasks.Queue{
		Name:       name,
		RateLimits: &cloudtasks.RateLimits{MaxDispatchesPerSecond: f64(dispatches)},
	}
}

func TestBuildPlan(t *testing.T) {
	file := &cloudtasks.File{
		Project:  "my-project",
		Location: "asia-northeast1",
		Queues: []*cloudtasks.Queue{
			queue("keep", 10),
			queue("changed", 20),
			queue("added", 30),
		},
	}
	actual := []*cloudtasks.Queue{
		queue("keep", 10),
		queue("changed", 500),
		queue("orphan", 40),
	}

	plan, err := cloudtasks.BuildPlan(file, actual)
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Create) != 1 || plan.Create[0].Name != "added" {
		t.Errorf("Create = %v, want [added]", names(plan.Create))
	}
	if len(plan.NoChange) != 1 || plan.NoChange[0].Name != "keep" {
		t.Errorf("NoChange = %v, want [keep]", names(plan.NoChange))
	}
	if len(plan.Delete) != 1 || plan.Delete[0].Name != "orphan" {
		t.Errorf("Delete = %v, want [orphan]", names(plan.Delete))
	}
	if len(plan.Update) != 1 {
		t.Fatalf("Update = %d 件, want 1 件", len(plan.Update))
	}
	u := plan.Update[0]
	if len(u.Changes) != 1 || u.Changes[0].String() != "~ rate_limits.max_dispatches_per_second: 500 => 20" {
		t.Errorf("Changes = %v, want max_dispatches_per_second の変更 1 件", u.Changes)
	}
	// キューには作り直しが必要な属性が無い。
	if u.RecreateRequired {
		t.Error("RecreateRequired = true, want false")
	}
}

func TestBuildPlan_zeroIsNotUnset(t *testing.T) {
	// 0 は「無制限」という意味のある値なので、未指定との違いが差分に出る。
	want := queue("q", 10)
	want.RateLimits.MaxConcurrentDispatches = i32(0)

	got := queue("q", 10)

	file := &cloudtasks.File{Project: "p", Location: "l", Queues: []*cloudtasks.Queue{want}}
	plan, err := cloudtasks.BuildPlan(file, []*cloudtasks.Queue{got})
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Update) != 1 {
		t.Fatalf("Update = %d 件, want 1 件", len(plan.Update))
	}
	if got, want := plan.Update[0].Changes[0].String(), "+ rate_limits.max_concurrent_dispatches: 0"; got != want {
		t.Errorf("Changes[0] = %q, want %q", got, want)
	}
}

func TestBuildPlan_ignoreChange(t *testing.T) {
	// Cloud Tasks が既定で埋める task_ttl と、運用側で変えた max_attempts を無視する。
	want := queue("q", 10)
	want.IgnoreChange = []string{"retry_config.max_attempts"}

	got := queue("q", 10)
	got.TaskTTL = "315576000000.999999999s"
	got.RetryConfig = &cloudtasks.RetryConfig{MaxAttempts: i32(-1)}

	file := &cloudtasks.File{
		Project:      "p",
		Location:     "l",
		IgnoreChange: []string{"task_ttl"},
		Queues:       []*cloudtasks.Queue{want},
	}
	plan, err := cloudtasks.BuildPlan(file, []*cloudtasks.Queue{got})
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Update) != 0 || len(plan.NoChange) != 1 {
		t.Fatalf("Update = %d 件, NoChange = %d 件, want 0 件 / 1 件", len(plan.Update), len(plan.NoChange))
	}
}

func TestLoadFile_invalid(t *testing.T) {
	cases := map[string]string{
		"未知のプロパティ": "queues:\n  - name: a\n    rate_limitsss: 1\n",
		"名前の重複":    "queues:\n  - name: a\n  - name: a\n",
		"名前が無い":    "queues:\n  - rate_limits: {max_dispatches_per_second: 1}\n",
		"認証が両方": "queues:\n  - name: a\n    http_target:\n" +
			"      oauth_token: {service_account_email: a@example.com}\n" +
			"      oidc_token: {service_account_email: a@example.com}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cloudtasks.yaml")
			if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := cloudtasks.LoadFile(path); err == nil {
				t.Error("LoadFile() error = nil, want error")
			}
		})
	}
}

func TestLoadFile_notExist(t *testing.T) {
	f, err := cloudtasks.LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadFile() error: %v", err)
	}
	if len(f.Queues) != 0 {
		t.Errorf("Queues = %d 件, want 0 件", len(f.Queues))
	}
}

func TestLoadFile_example(t *testing.T) {
	// README で案内しているサンプルが常にパースできることを確かめる。
	f, err := cloudtasks.LoadFile("../_example/cloudtasks.yaml")
	if err != nil {
		t.Fatalf("LoadFile() error: %v", err)
	}
	if len(f.Queues) == 0 {
		t.Fatal("サンプルにキューがありません")
	}
	for _, q := range f.Queues {
		if _, err := q.ToProto(f.Project, f.Location); err != nil {
			t.Errorf("queue %q: ToProto() error: %v", q.Name, err)
		}
	}
}

func names(queues []*cloudtasks.Queue) []string {
	out := make([]string, 0, len(queues))
	for _, q := range queues {
		out = append(out, q.Name)
	}
	return out
}

func TestBuildPlan_serverDefaults(t *testing.T) {
	// Cloud Tasks が既定値を埋めるプロパティは、API で未設定へ戻せない。
	// yaml に書かなかった場合は現状の値をそのまま使い、差分に出さない。
	want := &cloudtasks.Queue{
		Name:       "q",
		RateLimits: &cloudtasks.RateLimits{MaxDispatchesPerSecond: f64(10)},
	}
	got := &cloudtasks.Queue{
		Name: "q",
		RateLimits: &cloudtasks.RateLimits{
			MaxDispatchesPerSecond:  f64(10),
			MaxConcurrentDispatches: i32(1000),
		},
		RetryConfig: &cloudtasks.RetryConfig{
			MaxAttempts:  i32(100),
			MinBackoff:   "100ms",
			MaxBackoff:   "1h0m0s",
			MaxDoublings: i32(16),
		},
		TaskTTL:      "744h0m0s",
		TombstoneTTL: "1h0m0s",
	}

	file := &cloudtasks.File{Project: "p", Location: "l", Queues: []*cloudtasks.Queue{want}}
	plan, err := cloudtasks.BuildPlan(file, []*cloudtasks.Queue{got})
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.NoChange) != 1 {
		t.Fatalf("NoChange = %d 件, want 1 件 (changes: %v)", len(plan.NoChange), plan.Update)
	}
}

func TestBuildPlan_serverDefaultsAreStillUpdatable(t *testing.T) {
	// 書いたプロパティは既定値の対象でも普通に差分になる。
	want := &cloudtasks.Queue{
		Name:        "q",
		RetryConfig: &cloudtasks.RetryConfig{MaxAttempts: i32(5)},
		TaskTTL:     "240h0m0s",
	}
	got := &cloudtasks.Queue{
		Name:        "q",
		RetryConfig: &cloudtasks.RetryConfig{MaxAttempts: i32(100), MinBackoff: "100ms"},
		TaskTTL:     "744h0m0s",
	}

	file := &cloudtasks.File{Project: "p", Location: "l", Queues: []*cloudtasks.Queue{want}}
	plan, err := cloudtasks.BuildPlan(file, []*cloudtasks.Queue{got})
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Update) != 1 {
		t.Fatalf("Update = %d 件, want 1 件", len(plan.Update))
	}
	var lines []string
	for _, c := range plan.Update[0].Changes {
		lines = append(lines, c.String())
	}
	want0 := `~ retry_config.max_attempts: 100 => 5`
	want1 := `~ task_ttl: "744h0m0s" => "240h0m0s"`
	if len(lines) != 2 || lines[0] != want0 || lines[1] != want1 {
		t.Errorf("Changes = %v, want [%q %q]", lines, want0, want1)
	}
}

func TestBuildPlan_clearableFieldsStillDiff(t *testing.T) {
	// http_target は API で消せるので、yaml から外したら差分に出す。
	want := queue("q", 10)
	got := queue("q", 10)
	got.HTTPTarget = &cloudtasks.HTTPTarget{
		URIOverride: &cloudtasks.URIOverride{Host: "api.example.com"},
	}

	file := &cloudtasks.File{Project: "p", Location: "l", Queues: []*cloudtasks.Queue{want}}
	plan, err := cloudtasks.BuildPlan(file, []*cloudtasks.Queue{got})
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Update) != 1 || len(plan.Update[0].Changes) != 1 {
		t.Fatalf("Update = %#v, want http_target の削除 1 件", plan.Update)
	}
	if got := plan.Update[0].Changes[0].Path; got != "http_target" {
		t.Errorf("Changes[0].Path = %q, want http_target", got)
	}
}
