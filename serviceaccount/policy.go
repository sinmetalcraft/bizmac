package serviceaccount

import (
	"slices"
	"sort"

	crm "google.golang.org/api/cloudresourcemanager/v1"
	iamapi "google.golang.org/api/iam/v1"
)

// 条件付きの割り当て (IAM Conditions) は yaml で表現しないので、どの操作でも読み飛ばして
// そのまま残す。条件付きか否かでバインディングを分けて扱うのはこのため。

// memberRoles はプロジェクトのポリシーから、member に条件なしで付いているロールを返す。
// conditional は member に条件付きで付いているロール。
func memberRoles(p *crm.Policy, member string) (roles, conditional []string) {
	for _, b := range p.Bindings {
		if !slices.Contains(b.Members, member) {
			continue
		}
		if b.Condition != nil {
			conditional = append(conditional, b.Role)
			continue
		}
		roles = append(roles, b.Role)
	}
	return sortedUnique(roles), sortedUnique(conditional)
}

// setMemberRoles はプロジェクトのポリシーの条件なしバインディングについて、member が
// roles のロールにだけ含まれるように書き換える。他のメンバーや条件付きバインディングは
// 触らない。変更があれば true を返す。
func setMemberRoles(p *crm.Policy, member string, roles []string) bool {
	want := make(map[string]bool, len(roles))
	for _, r := range roles {
		want[r] = true
	}
	// placed は member を含む条件なしバインディングを 1 つ確保したロール。
	// 同じロールの条件なしバインディングが複数あれば、2 つ目以降からは外して 1 つにまとめる。
	placed := map[string]bool{}
	changed := false
	var bindings []*crm.Binding
	for _, b := range p.Bindings {
		if b.Condition != nil {
			bindings = append(bindings, b)
			continue
		}
		has := slices.Contains(b.Members, member)
		keep := want[b.Role] && !placed[b.Role]
		switch {
		case keep && !has:
			b.Members = append(b.Members, member)
			changed = true
		case !keep && has:
			b.Members = slices.DeleteFunc(b.Members, func(m string) bool { return m == member })
			changed = true
		}
		if keep {
			placed[b.Role] = true
		}
		if len(b.Members) > 0 {
			bindings = append(bindings, b)
		}
	}
	// ポリシーにまだ無いロールはバインディングを作る。
	for _, r := range sortedKeys(want) {
		if placed[r] {
			continue
		}
		bindings = append(bindings, &crm.Binding{Role: r, Members: []string{member}})
		changed = true
	}
	p.Bindings = bindings
	return changed
}

// policyMembers はサービスアカウントのポリシーを ロール → メンバー の map にして返す。
// conditional は条件付きバインディングの件数。
func policyMembers(p *iamapi.Policy) (members map[string][]string, conditional int) {
	for _, b := range p.Bindings {
		if b.Condition != nil {
			conditional++
			continue
		}
		if len(b.Members) == 0 {
			continue
		}
		if members == nil {
			members = map[string][]string{}
		}
		members[b.Role] = sortedUnique(append(members[b.Role], b.Members...))
	}
	return members, conditional
}

// setPolicyMembers はサービスアカウントのポリシーの条件なしバインディングを want の
// とおりに置き換える。条件付きバインディングは残す。変更があれば true を返す。
func setPolicyMembers(p *iamapi.Policy, want map[string][]string) bool {
	current, _ := policyMembers(p)
	if equalMembers(current, want) {
		return false
	}
	var bindings []*iamapi.Binding
	for _, b := range p.Bindings {
		if b.Condition != nil {
			bindings = append(bindings, b)
		}
	}
	for _, r := range sortedKeys(want) {
		if len(want[r]) == 0 {
			continue
		}
		bindings = append(bindings, &iamapi.Binding{Role: r, Members: sortedUnique(want[r])})
	}
	p.Bindings = bindings
	return true
}

func equalMembers(a, b map[string][]string) bool {
	norm := func(m map[string][]string) map[string][]string {
		out := map[string][]string{}
		for r, ms := range m {
			if u := sortedUnique(ms); u != nil {
				out[r] = u
			}
		}
		return out
	}
	na, nb := norm(a), norm(b)
	if len(na) != len(nb) {
		return false
	}
	for r, ms := range na {
		if !slices.Equal(ms, nb[r]) {
			return false
		}
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
