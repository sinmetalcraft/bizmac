package scheduler_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sinmetalcraft/bizmac/scheduler"
)

func httpJob(name, schedule string) *scheduler.Job {
	return &scheduler.Job{
		Name:     name,
		Schedule: schedule,
		HTTPTarget: &scheduler.HTTPTarget{
			URI:        "https://example.com/" + name,
			HTTPMethod: "POST",
		},
	}
}

func TestBuildPlan(t *testing.T) {
	file := &scheduler.File{
		Project:  "my-project",
		Location: "asia-northeast1",
		Jobs: []*scheduler.Job{
			httpJob("keep", "0 1 * * *"),
			httpJob("changed", "0 2 * * *"),
			httpJob("added", "0 3 * * *"),
		},
	}
	actual := []*scheduler.Job{
		httpJob("keep", "0 1 * * *"),
		httpJob("changed", "0 9 * * *"),
		httpJob("orphan", "0 4 * * *"),
	}

	plan, err := scheduler.BuildPlan(file, actual)
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
	if len(u.Changes) != 1 || u.Changes[0].String() != `~ schedule: "0 9 * * *" => "0 2 * * *"` {
		t.Errorf("Changes = %v, want schedule の変更 1 件", u.Changes)
	}
	if !plan.HasChange() {
		t.Error("HasChange() = false, want true")
	}
}

func TestBuildPlan_ignoreChange(t *testing.T) {
	// Cloud Scheduler が勝手に付ける User-Agent と、運用側で変えている description を無視する。
	want := httpJob("job", "0 1 * * *")
	want.IgnoreChange = []string{"http_target.headers.User-Agent"}

	got := httpJob("job", "0 1 * * *")
	got.Description = "コンソールで編集された説明"
	got.HTTPTarget.Headers = map[string]string{"User-Agent": "Google-Cloud-Scheduler"}

	file := &scheduler.File{
		Project:      "my-project",
		Location:     "asia-northeast1",
		IgnoreChange: []string{"description"},
		Jobs:         []*scheduler.Job{want},
	}

	plan, err := scheduler.BuildPlan(file, []*scheduler.Job{got})
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Update) != 0 || len(plan.NoChange) != 1 {
		t.Fatalf("Update = %d 件, NoChange = %d 件, want 0 件 / 1 件 (changes: %v)",
			len(plan.Update), len(plan.NoChange), updateChanges(plan))
	}
}

func TestBuildPlan_ignoredPropertyIsNotOverwritten(t *testing.T) {
	// 無視対象以外に差分があるとき、送信する定義には Google Cloud 側の値が残る。
	want := httpJob("job", "0 2 * * *")
	want.IgnoreChange = []string{"description"}

	got := httpJob("job", "0 1 * * *")
	got.Description = "コンソールで編集された説明"

	file := &scheduler.File{Project: "p", Location: "l", Jobs: []*scheduler.Job{want}}
	plan, err := scheduler.BuildPlan(file, []*scheduler.Job{got})
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Update) != 1 {
		t.Fatalf("Update = %d 件, want 1 件", len(plan.Update))
	}
	u := plan.Update[0]
	if u.Desired.Description != got.Description {
		t.Errorf("Desired.Description = %q, want %q", u.Desired.Description, got.Description)
	}
	if len(u.Changes) != 1 {
		t.Errorf("Changes = %v, want schedule の変更 1 件だけ", u.Changes)
	}
	if u.Desired.IgnoreChange != nil {
		t.Errorf("Desired.IgnoreChange = %v, want nil", u.Desired.IgnoreChange)
	}
}

func TestBuildPlan_bodyAndBodyJSONAreEquivalent(t *testing.T) {
	want := httpJob("job", "0 1 * * *")
	want.HTTPTarget.Body = `{"kind": "batch", "target": "users"}`

	got := httpJob("job", "0 1 * * *")
	got.HTTPTarget.BodyJSON = map[string]any{"target": "users", "kind": "batch"}

	file := &scheduler.File{Project: "p", Location: "l", Jobs: []*scheduler.Job{want}}
	plan, err := scheduler.BuildPlan(file, []*scheduler.Job{got})
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.NoChange) != 1 {
		t.Errorf("NoChange = %d 件, want 1 件 (changes: %v)", len(plan.NoChange), updateChanges(plan))
	}
}

func TestBuildPlan_targetKindChanged(t *testing.T) {
	want := httpJob("job", "0 1 * * *")
	got := &scheduler.Job{
		Name:         "job",
		Schedule:     "0 1 * * *",
		PubsubTarget: &scheduler.PubsubTarget{TopicName: "projects/p/topics/t"},
	}

	file := &scheduler.File{Project: "p", Location: "l", Jobs: []*scheduler.Job{want}}
	plan, err := scheduler.BuildPlan(file, []*scheduler.Job{got})
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Update) != 1 || !plan.Update[0].RecreateRequired {
		t.Errorf("RecreateRequired が検出されませんでした: %+v", plan.Update)
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduler.yaml")
	src := `project: my-project
location: asia-northeast1
ignore_change:
  - description
jobs:
  - name: nightly-batch
    schedule: "0 3 * * *"
    time_zone: Asia/Tokyo
    ignore_change:
      - http_target.headers.User-Agent
    http_target:
      uri: https://example.com/batch
      http_method: POST
      body_json:
        kind: batch
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := scheduler.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error: %v", err)
	}
	if f.Project != "my-project" || f.Location != "asia-northeast1" {
		t.Errorf("project/location = %q/%q", f.Project, f.Location)
	}
	if len(f.Jobs) != 1 {
		t.Fatalf("Jobs = %d 件, want 1 件", len(f.Jobs))
	}
	j := f.Jobs[0]
	if j.HTTPTarget.BodyJSON == nil {
		t.Error("body_json が読めていません")
	}
	if len(j.IgnoreChange) != 1 || len(f.IgnoreChange) != 1 {
		t.Errorf("ignore_change = %v / %v", f.IgnoreChange, j.IgnoreChange)
	}
}

func TestLoadFile_notExist(t *testing.T) {
	f, err := scheduler.LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadFile() error: %v", err)
	}
	if len(f.Jobs) != 0 {
		t.Errorf("Jobs = %d 件, want 0 件", len(f.Jobs))
	}
}

func TestLoadFile_invalid(t *testing.T) {
	cases := map[string]string{
		"未知のプロパティ": "jobs:\n  - name: a\n    scheduleeee: x\n",
		"名前の重複": "jobs:\n  - name: a\n    http_target: {uri: https://example.com}\n" +
			"  - name: a\n    http_target: {uri: https://example.com}\n",
		"ターゲットが無い": "jobs:\n  - name: a\n    schedule: \"0 1 * * *\"\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "scheduler.yaml")
			if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := scheduler.LoadFile(path); err == nil {
				t.Error("LoadFile() error = nil, want error")
			}
		})
	}
}

func names(jobs []*scheduler.Job) []string {
	out := make([]string, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.Name)
	}
	return out
}

func updateChanges(p *scheduler.Plan) []string {
	var out []string
	for _, u := range p.Update {
		for _, c := range u.Changes {
			out = append(out, u.Name+": "+c.String())
		}
	}
	return out
}

func TestLoadFile_example(t *testing.T) {
	// README で案内しているサンプルが常にパースできることを確かめる。
	f, err := scheduler.LoadFile("../_example/scheduler.yaml")
	if err != nil {
		t.Fatalf("LoadFile() error: %v", err)
	}
	if len(f.Jobs) == 0 {
		t.Fatal("サンプルにジョブがありません")
	}
	for _, j := range f.Jobs {
		if _, err := j.ToProto(f.Project, f.Location); err != nil {
			t.Errorf("job %q: ToProto() error: %v", j.Name, err)
		}
	}
}
