package cli

import (
	"strings"
	"testing"

	"github.com/sinmetalcraft/bizmac/prop"
	"github.com/sinmetalcraft/bizmac/scheduler"
)

func TestPrintPlan(t *testing.T) {
	plan := &scheduler.Plan{
		Project:  "my-project",
		Location: "asia-northeast1",
		Create: []*scheduler.Job{{
			Name:       "added",
			Schedule:   "0 3 * * *",
			HTTPTarget: &scheduler.HTTPTarget{URI: "https://example.com/added", HTTPMethod: "POST"},
		}},
		Update: []*scheduler.JobUpdate{{
			Name:    "changed",
			Changes: []prop.Change{{Path: "schedule", Type: prop.Modified, Old: "0 9 * * *", New: "0 2 * * *"}},
		}},
		Delete:   []*scheduler.Job{{Name: "orphan"}},
		NoChange: []*scheduler.Job{{Name: "keep"}},
	}

	var sb strings.Builder
	if err := printPlan(&sb, plan); err != nil {
		t.Fatalf("printPlan() error: %v", err)
	}
	got := sb.String()

	want := []string{
		"project:  my-project",
		"location: asia-northeast1",
		"+ create added",
		"    - name: added",
		"        uri: https://example.com/added",
		"~ update changed",
		`    ~ schedule: "0 9 * * *" => "0 2 * * *"`,
		"- vacuum orphan (yaml に定義がありません)",
		"create: 1, update: 1, no change: 1, vacuum candidate: 1",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("printPlan() の出力に %q がありません:\n%s", w, got)
		}
	}
}

func TestPrintPlan_noChange(t *testing.T) {
	var sb strings.Builder
	if err := printPlan(&sb, &scheduler.Plan{Project: "p", Location: "l"}); err != nil {
		t.Fatalf("printPlan() error: %v", err)
	}
	if !strings.Contains(sb.String(), "差分はありません。") {
		t.Errorf("printPlan() = %q", sb.String())
	}
}

func TestConfirm(t *testing.T) {
	cases := map[string]bool{"y\n": true, "Y\n": true, "yes\n": true, "n\n": false, "\n": false, "": false}
	for in, want := range cases {
		var out strings.Builder
		got, err := confirm(strings.NewReader(in), &out, "削除しますか?")
		if err != nil {
			t.Fatalf("confirm(%q) error: %v", in, err)
		}
		if got != want {
			t.Errorf("confirm(%q) = %v, want %v", in, got, want)
		}
	}
}
