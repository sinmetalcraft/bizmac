package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildFilePlan(t *testing.T) {
	dir := t.TempDir()
	dev := writeFile(t, dir, "dev.yaml", `project: dev-project
location: asia-northeast1
jobs:
  - name: same
    schedule: "0 1 * * *"
    http_target:
      uri: https://example.com/same
  - name: different
    schedule: "0 2 * * *"
    http_target:
      uri: https://example.com/different
  - name: dev-only
    schedule: "0 3 * * *"
    http_target:
      uri: https://example.com/dev-only
`)
	prod := writeFile(t, dir, "prod.yaml", `project: prod-project
location: asia-northeast1
jobs:
  - name: same
    schedule: "0 1 * * *"
    http_target:
      uri: https://example.com/same
  - name: different
    schedule: "0 9 * * *"
    http_target:
      uri: https://example.com/different
  - name: prod-only
    schedule: "0 4 * * *"
    http_target:
      uri: https://example.com/prod-only
`)

	fp, err := buildFilePlan(schedulerKind(), dev, prod)
	if err != nil {
		t.Fatalf("buildFilePlan() error: %v", err)
	}
	p := fp.plan
	if len(p.Create) != 1 || p.Create[0].Name != "dev-only" {
		t.Errorf("Create = %v, want [dev-only]", p.Create)
	}
	if len(p.Delete) != 1 || p.Delete[0].Name != "prod-only" {
		t.Errorf("Delete = %v, want [prod-only]", p.Delete)
	}
	if len(p.NoChange) != 1 || p.NoChange[0].Name != "same" {
		t.Errorf("NoChange = %v, want [same]", p.NoChange)
	}
	if len(p.Update) != 1 || len(p.Update[0].Changes) != 1 {
		t.Fatalf("Update = %#v, want schedule の差分 1 件", p.Update)
	}
	// --against 側が現状、--file 側があるべき姿。
	if got, want := p.Update[0].Changes[0].String(), `~ schedule: "0 9 * * *" => "0 2 * * *"`; got != want {
		t.Errorf("Changes[0] = %q, want %q", got, want)
	}

	var sb strings.Builder
	if err := printFilePlan(&sb, dev, prod, fp); err != nil {
		t.Fatalf("printFilePlan() error: %v", err)
	}
	for _, want := range []string{
		"project: dev-project",
		"project: prod-project",
		"+ dev-only (--file にのみあります)",
		"~ different",
		"- prod-only (--against にのみあります)",
		"only in --file: 1, different: 1, same: 1, only in --against: 1",
	} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("printFilePlan() の出力に %q がありません:\n%s", want, sb.String())
		}
	}
}

func TestBuildFilePlan_ignoreChangeFromBothFiles(t *testing.T) {
	// ignore_change はどちらのファイルに書いても効く。
	dir := t.TempDir()
	dev := writeFile(t, dir, "dev.yaml", `jobs:
  - name: a
    description: dev の説明
    schedule: "0 1 * * *"
    ignore_change:
      - description
    http_target:
      uri: https://dev.example.com/a
  - name: b
    time_zone: Asia/Tokyo
    schedule: "0 1 * * *"
    http_target:
      uri: https://example.com/b
`)
	prod := writeFile(t, dir, "prod.yaml", `ignore_change:
  - http_target.uri
jobs:
  - name: a
    description: prod の説明
    schedule: "0 1 * * *"
    http_target:
      uri: https://prod.example.com/a
  - name: b
    time_zone: UTC
    schedule: "0 1 * * *"
    ignore_change:
      - time_zone
    http_target:
      uri: https://example.com/b
`)

	fp, err := buildFilePlan(schedulerKind(), dev, prod)
	if err != nil {
		t.Fatalf("buildFilePlan() error: %v", err)
	}
	if len(fp.plan.NoChange) != 2 {
		t.Errorf("NoChange = %d 件, want 2 件 (update: %#v)", len(fp.plan.NoChange), fp.plan.Update)
	}
}

func TestBuildFilePlan_missingFile(t *testing.T) {
	dir := t.TempDir()
	dev := writeFile(t, dir, "dev.yaml", "jobs: []\n")
	if _, err := buildFilePlan(schedulerKind(), dev, filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("buildFilePlan() error = nil, want error")
	}
}
