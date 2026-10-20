package serviceaccount_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sinmetalcraft/bizmac/prop"
	"github.com/sinmetalcraft/bizmac/serviceaccount"
)

func changeLines(changes []prop.Change) []string {
	lines := make([]string, 0, len(changes))
	for _, c := range changes {
		lines = append(lines, c.String())
	}
	return lines
}

func TestBuildPlan(t *testing.T) {
	file := &serviceaccount.File{
		Project: "p",
		ServiceAccounts: []*serviceaccount.ServiceAccount{
			{Name: "keep-agent", ProjectRoles: []string{"roles/b", "roles/a"}},
			{Name: "new-agent", ProjectRoles: []string{"roles/a"}},
		},
	}
	actual := []*serviceaccount.ServiceAccount{
		{Name: "keep-agent", ProjectRoles: []string{"roles/a", "roles/b"}},
	}

	plan, err := serviceaccount.BuildPlan(file, actual)
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Create) != 1 || plan.Create[0].Name != "new-agent" {
		t.Errorf("Create = %v, want [new-agent]", plan.Create)
	}
	// 並び順だけの違いは差分にならない。
	if len(plan.NoChange) != 1 || plan.NoChange[0].Name != "keep-agent" {
		t.Errorf("NoChange = %v, want [keep-agent]", plan.NoChange)
	}
	if len(plan.Update) != 0 || len(plan.Revoke) != 0 || len(plan.Delete) != 0 {
		t.Errorf("Update = %d, Revoke = %d, Delete = %d, want すべて 0",
			len(plan.Update), len(plan.Revoke), len(plan.Delete))
	}
}

func TestBuildPlan_revokeIsVacuum(t *testing.T) {
	// yaml から外れた割り当ては update では取り消さず、vacuum に回る。
	file := &serviceaccount.File{
		Project: "p",
		ServiceAccounts: []*serviceaccount.ServiceAccount{{
			Name:         "ai-agent",
			DisplayName:  "AI Agent",
			ProjectRoles: []string{"roles/bigquery.dataViewer", "roles/spanner.viewer"},
			IAMPolicy: map[string][]string{
				"roles/iam.serviceAccountTokenCreator": {"user:a@example.com"},
			},
		}},
	}
	actual := []*serviceaccount.ServiceAccount{{
		Name:         "ai-agent",
		ProjectRoles: []string{"roles/bigquery.dataViewer", "roles/iam.serviceAccountTokenCreator"},
		IAMPolicy: map[string][]string{
			"roles/iam.serviceAccountTokenCreator": {"user:a@example.com", "user:b@example.com"},
		},
	}}

	plan, err := serviceaccount.BuildPlan(file, actual)
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Update) != 1 || len(plan.Revoke) != 1 {
		t.Fatalf("Update = %d 件, Revoke = %d 件, want 1 件 / 1 件", len(plan.Update), len(plan.Revoke))
	}

	u := plan.Update[0]
	wantUpdate := []string{
		`+ display_name: "AI Agent"`,
		`+ project_roles: "roles/spanner.viewer"`,
	}
	if got := changeLines(u.Changes); !reflect.DeepEqual(got, wantUpdate) {
		t.Errorf("Update.Changes = %q, want %q", got, wantUpdate)
	}
	// update で送る定義は、取り消し対象を残したまま追加だけを含む。
	wantRoles := []string{"roles/bigquery.dataViewer", "roles/iam.serviceAccountTokenCreator", "roles/spanner.viewer"}
	if !reflect.DeepEqual(u.Desired.ProjectRoles, wantRoles) {
		t.Errorf("Update.Desired.ProjectRoles = %v, want %v", u.Desired.ProjectRoles, wantRoles)
	}
	wantMembers := []string{"user:a@example.com", "user:b@example.com"}
	if got := u.Desired.IAMPolicy["roles/iam.serviceAccountTokenCreator"]; !reflect.DeepEqual(got, wantMembers) {
		t.Errorf("Update.Desired.IAMPolicy = %v, want %v", got, wantMembers)
	}

	r := plan.Revoke[0]
	wantRevoke := []string{
		`- iam_policy.roles/iam.serviceAccountTokenCreator: "user:b@example.com"`,
		`- project_roles: "roles/iam.serviceAccountTokenCreator"`,
	}
	if got := changeLines(r.Changes); !reflect.DeepEqual(got, wantRevoke) {
		t.Errorf("Revoke.Changes = %q, want %q", got, wantRevoke)
	}
	// vacuum で送る定義は、現状から取り消し対象を除いただけで追加は含まない。
	if want := []string{"roles/bigquery.dataViewer"}; !reflect.DeepEqual(r.Desired.ProjectRoles, want) {
		t.Errorf("Revoke.Desired.ProjectRoles = %v, want %v", r.Desired.ProjectRoles, want)
	}
	if r.Desired.DisplayName != "" {
		t.Errorf("Revoke.Desired.DisplayName = %q, want 現状のまま空", r.Desired.DisplayName)
	}
	if want := map[string][]string{"roles/iam.serviceAccountTokenCreator": {"user:a@example.com"}}; !reflect.DeepEqual(r.Desired.IAMPolicy, want) {
		t.Errorf("Revoke.Desired.IAMPolicy = %v, want %v", r.Desired.IAMPolicy, want)
	}
}

func TestBuildPlan_revokeOnly(t *testing.T) {
	// 取り消しだけの場合は update の対象にならない。
	file := &serviceaccount.File{Project: "p", ServiceAccounts: []*serviceaccount.ServiceAccount{{Name: "ai-agent"}}}
	actual := []*serviceaccount.ServiceAccount{{
		Name:      "ai-agent",
		IAMPolicy: map[string][]string{"roles/iam.serviceAccountUser": {"user:a@example.com"}},
	}}

	plan, err := serviceaccount.BuildPlan(file, actual)
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if plan.HasChange() || len(plan.NoChange) != 0 {
		t.Errorf("HasChange() = %v, NoChange = %d 件, want false / 0 件", plan.HasChange(), len(plan.NoChange))
	}
	if len(plan.Revoke) != 1 || plan.Revoke[0].Desired.IAMPolicy != nil {
		t.Errorf("Revoke = %+v, want ai-agent の iam_policy を空にする 1 件", plan.Revoke)
	}
}

func TestBuildPlan_ignoreChange(t *testing.T) {
	file := &serviceaccount.File{
		Project:      "p",
		IgnoreChange: []string{"description"},
		ServiceAccounts: []*serviceaccount.ServiceAccount{{
			Name:         "ai-agent",
			IgnoreChange: []string{"iam_policy"},
		}},
	}
	actual := []*serviceaccount.ServiceAccount{{
		Name:        "ai-agent",
		Description: "手で書いた説明",
		IAMPolicy:   map[string][]string{"roles/iam.serviceAccountUser": {"user:a@example.com"}},
	}}

	plan, err := serviceaccount.BuildPlan(file, actual)
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.NoChange) != 1 || len(plan.Revoke) != 0 {
		t.Errorf("NoChange = %d 件, Revoke = %d 件, want 1 件 / 0 件", len(plan.NoChange), len(plan.Revoke))
	}
}

func TestLoadFile_invalid(t *testing.T) {
	cases := map[string]string{
		"未知のプロパティ":    "service_accounts:\n  - name: ai-agent\n    roles: [roles/viewer]\n",
		"名前の重複":       "service_accounts:\n  - name: ai-agent\n  - name: ai-agent\n",
		"名前が無い":       "service_accounts:\n  - display_name: x\n",
		"メールアドレスで書いた": "service_accounts:\n  - name: ai-agent@p.iam.gserviceaccount.com\n",
		"名前が短い":       "service_accounts:\n  - name: ai\n",
		"ロールの書式":      "service_accounts:\n  - name: ai-agent\n    project_roles: [viewer]\n",
		"メンバーの書式": "service_accounts:\n  - name: ai-agent\n    iam_policy:\n" +
			"      roles/iam.serviceAccountTokenCreator: [a@example.com]\n",
		"location は無い": "project: p\nlocation: asia-northeast1\nservice_accounts: []\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "serviceaccount.yaml")
			if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := serviceaccount.LoadFile(path); err == nil {
				t.Error("LoadFile() error = nil, want error")
			}
		})
	}
}

func TestLoadFile_example(t *testing.T) {
	// README で案内しているサンプルが常にパースできることを確かめる。
	f, err := serviceaccount.LoadFile("../_example/serviceaccount.yaml")
	if err != nil {
		t.Fatalf("LoadFile() error: %v", err)
	}
	if len(f.ServiceAccounts) == 0 {
		t.Fatal("サンプルにサービスアカウントがありません")
	}
}
