package serviceaccount

import (
	"reflect"
	"testing"

	crm "google.golang.org/api/cloudresourcemanager/v1"
	iamapi "google.golang.org/api/iam/v1"
)

const agent = "serviceAccount:ai-agent@p.iam.gserviceaccount.com"

func TestSetMemberRoles(t *testing.T) {
	cond := &crm.Expr{Expression: "request.time < timestamp('2030-01-01T00:00:00Z')"}
	p := &crm.Policy{Bindings: []*crm.Binding{
		{Role: "roles/viewer", Members: []string{"user:a@example.com", agent}},
		{Role: "roles/editor", Members: []string{agent}},
		{Role: "roles/spanner.viewer", Members: []string{"user:a@example.com"}},
		// 条件付きの割り当ては触らない。
		{Role: "roles/owner", Members: []string{agent}, Condition: cond},
	}}

	if !setMemberRoles(p, agent, []string{"roles/spanner.viewer", "roles/bigquery.jobUser"}) {
		t.Fatal("setMemberRoles() = false, want true")
	}
	want := []*crm.Binding{
		// 他のメンバーは残る。
		{Role: "roles/viewer", Members: []string{"user:a@example.com"}},
		// メンバーが空になったバインディングは消える (roles/editor)。
		{Role: "roles/spanner.viewer", Members: []string{"user:a@example.com", agent}},
		{Role: "roles/owner", Members: []string{agent}, Condition: cond},
		{Role: "roles/bigquery.jobUser", Members: []string{agent}},
	}
	if !reflect.DeepEqual(p.Bindings, want) {
		t.Errorf("Bindings = %+v, want %+v", dumpBindings(p.Bindings), dumpBindings(want))
	}

	roles, conditional := memberRoles(p, agent)
	if want := []string{"roles/bigquery.jobUser", "roles/spanner.viewer"}; !reflect.DeepEqual(roles, want) {
		t.Errorf("memberRoles() roles = %v, want %v", roles, want)
	}
	if want := []string{"roles/owner"}; !reflect.DeepEqual(conditional, want) {
		t.Errorf("memberRoles() conditional = %v, want %v", conditional, want)
	}

	// もう一度同じ内容を適用しても変更は無い。
	if setMemberRoles(p, agent, []string{"roles/spanner.viewer", "roles/bigquery.jobUser"}) {
		t.Error("2 回目の setMemberRoles() = true, want false")
	}
}

func TestSetMemberRoles_duplicateBindings(t *testing.T) {
	// 同じロールの条件なしバインディングが複数あっても、メンバーは 1 か所にまとまる。
	p := &crm.Policy{Bindings: []*crm.Binding{
		{Role: "roles/viewer", Members: []string{"user:a@example.com"}},
		{Role: "roles/viewer", Members: []string{agent}},
	}}
	setMemberRoles(p, agent, []string{"roles/viewer"})
	want := []*crm.Binding{
		{Role: "roles/viewer", Members: []string{"user:a@example.com", agent}},
	}
	if !reflect.DeepEqual(p.Bindings, want) {
		t.Errorf("Bindings = %+v, want %+v", dumpBindings(p.Bindings), dumpBindings(want))
	}
}

func TestSetPolicyMembers(t *testing.T) {
	cond := &iamapi.Expr{Expression: "true"}
	p := &iamapi.Policy{Bindings: []*iamapi.Binding{
		{Role: "roles/iam.serviceAccountUser", Members: []string{"user:a@example.com"}},
		{Role: "roles/iam.serviceAccountTokenCreator", Members: []string{"user:b@example.com"}, Condition: cond},
	}}
	want := map[string][]string{"roles/iam.serviceAccountTokenCreator": {"user:c@example.com", "user:a@example.com"}}

	if !setPolicyMembers(p, want) {
		t.Fatal("setPolicyMembers() = false, want true")
	}
	got, conditional := policyMembers(p)
	if exp := map[string][]string{"roles/iam.serviceAccountTokenCreator": {"user:a@example.com", "user:c@example.com"}}; !reflect.DeepEqual(got, exp) {
		t.Errorf("policyMembers() = %v, want %v", got, exp)
	}
	if conditional != 1 {
		t.Errorf("conditional = %d, want 1 (条件付きは残る)", conditional)
	}
	if setPolicyMembers(p, want) {
		t.Error("2 回目の setPolicyMembers() = true, want false")
	}
}

func dumpBindings[B any](bs []*B) []B {
	out := make([]B, 0, len(bs))
	for _, b := range bs {
		out = append(out, *b)
	}
	return out
}
