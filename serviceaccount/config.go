// Package serviceaccount はサービスアカウントと、それに関わる IAM の割り当てを yaml で管理するための
// 設定ファイルの読み書きと、Google Cloud との差分計算を提供する。
//
// 管理するのは yaml に書いたサービスアカウントだけで、プロジェクトの IAM ポリシーも
// そのサービスアカウントがメンバーになっている割り当てだけを見る。
// 他の経路で付いた割り当てや、yaml に書いていないサービスアカウントには触らない。
package serviceaccount

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultFileName は serviceaccount リソースの既定の設定ファイル名。
const DefaultFileName = "serviceaccount.yaml"

// File は serviceaccount.yaml 1 ファイル分の内容。
// IAM はロケーションを持たないので location は無い。
type File struct {
	Project string `yaml:"project,omitempty"`
	// IgnoreChange は全サービスアカウントに適用される、変更を無視するプロパティパス。
	IgnoreChange    []string          `yaml:"ignore_change,omitempty"`
	ServiceAccounts []*ServiceAccount `yaml:"service_accounts"`
}

// ServiceAccount はサービスアカウント 1 件と、それに関わる IAM の割り当て。
type ServiceAccount struct {
	// Name はアカウント ID。メールアドレスの @ より前だけを書く。
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name,omitempty"`
	Description string `yaml:"description,omitempty"`
	// IgnoreChange はこのサービスアカウントにだけ追加で適用される無視プロパティパス。
	IgnoreChange []string `yaml:"ignore_change,omitempty"`
	// ProjectRoles はプロジェクト単位でこのサービスアカウントに付けるロール。
	ProjectRoles []string `yaml:"project_roles,omitempty"`
	// IAMPolicy はこのサービスアカウント自体に対する割り当て。ロール → メンバーの map。
	// 誰がこのサービスアカウントになりすませるか、などを書く。
	IAMPolicy map[string][]string `yaml:"iam_policy,omitempty"`
}

// accountIDPattern はサービスアカウントのアカウント ID の書式。
var accountIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

// LoadFile は設定ファイルを読み込む。ファイルが無い場合は空の File を返す。
func LoadFile(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &File{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("serviceaccount: read %s: %w", path, err)
	}
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("serviceaccount: parse %s: %w", path, err)
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("serviceaccount: %s: %w", path, err)
	}
	return &f, nil
}

// Validate は設定ファイルの内容を検証する。
func (f *File) Validate() error {
	seen := map[string]struct{}{}
	for i, sa := range f.ServiceAccounts {
		if sa == nil {
			return fmt.Errorf("service_accounts[%d] is empty", i)
		}
		if sa.Name == "" {
			return fmt.Errorf("service_accounts[%d] has no name", i)
		}
		if _, ok := seen[sa.Name]; ok {
			return fmt.Errorf("service account %q is defined more than once", sa.Name)
		}
		seen[sa.Name] = struct{}{}
		if err := sa.Validate(); err != nil {
			return fmt.Errorf("service account %q: %w", sa.Name, err)
		}
	}
	return nil
}

// Validate はサービスアカウント 1 件の内容を検証する。
func (sa *ServiceAccount) Validate() error {
	if strings.Contains(sa.Name, "@") {
		return fmt.Errorf("name にはメールアドレスではなくアカウント ID (%s) を書いてください",
			sa.Name[:strings.Index(sa.Name, "@")])
	}
	if !accountIDPattern.MatchString(sa.Name) {
		return fmt.Errorf("name must be 6-30 characters of lowercase letters, digits and hyphens, " +
			"starting with a letter")
	}
	for _, r := range sa.ProjectRoles {
		if err := validateRole(r); err != nil {
			return fmt.Errorf("project_roles: %w", err)
		}
	}
	for r, members := range sa.IAMPolicy {
		if err := validateRole(r); err != nil {
			return fmt.Errorf("iam_policy: %w", err)
		}
		for _, m := range members {
			if !strings.Contains(m, ":") {
				return fmt.Errorf("iam_policy.%s: member %q must have a type prefix such as user: or serviceAccount:", r, m)
			}
		}
	}
	return nil
}

// validateRole はロール名が roles/xxx かカスタムロールのフルネームであることを確かめる。
func validateRole(role string) error {
	for _, prefix := range []string{"roles/", "projects/", "organizations/"} {
		if strings.HasPrefix(role, prefix) {
			return nil
		}
	}
	return fmt.Errorf("role %q must start with roles/, projects/ or organizations/", role)
}

// Email はサービスアカウントのメールアドレスを返す。
func Email(project, name string) string {
	return name + "@" + project + ".iam.gserviceaccount.com"
}

// Member はサービスアカウントを IAM ポリシーのメンバーとして書いたときの表現を返す。
func Member(project, name string) string {
	return "serviceAccount:" + Email(project, name)
}

// SortServiceAccounts はサービスアカウントを名前順に並べ替える。
func (f *File) SortServiceAccounts() {
	sort.Slice(f.ServiceAccounts, func(i, k int) bool {
		return f.ServiceAccounts[i].Name < f.ServiceAccounts[k].Name
	})
}

// Save は設定ファイルを書き出す。path が "-" の場合は標準出力へ書く。
func (f *File) Save(path string) error {
	b, err := f.Marshal()
	if err != nil {
		return err
	}
	if path == "-" {
		_, err := os.Stdout.Write(b)
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("serviceaccount: mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("serviceaccount: write %s: %w", path, err)
	}
	return nil
}

// Marshal は設定ファイルを yaml へエンコードする。
func (f *File) Marshal() ([]byte, error) {
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, fmt.Errorf("serviceaccount: encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("serviceaccount: encode yaml: %w", err)
	}
	return []byte(sb.String()), nil
}

// sortedUnique は重複を除いて並べ替えたコピーを返す。空なら nil を返す。
func sortedUnique(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// ItemName は resource.Item の実装。
func (sa *ServiceAccount) ItemName() string { return sa.Name }

// ItemIgnoreChange は resource.Item の実装。
func (sa *ServiceAccount) ItemIgnoreChange() []string { return sa.IgnoreChange }

// SetItemIgnoreChange は resource.Item の実装。
func (sa *ServiceAccount) SetItemIgnoreChange(paths []string) { sa.IgnoreChange = paths }

// Canonicalize は resource.Item の実装。ロールとメンバーは順序に意味が無いので並べ替える。
func (sa *ServiceAccount) Canonicalize() error {
	sa.ProjectRoles = sortedUnique(sa.ProjectRoles)
	for r, members := range sa.IAMPolicy {
		if m := sortedUnique(members); m != nil {
			sa.IAMPolicy[r] = m
		} else {
			delete(sa.IAMPolicy, r)
		}
	}
	if len(sa.IAMPolicy) == 0 {
		sa.IAMPolicy = nil
	}
	return nil
}

// RecreateKey は resource.Item の実装。サービスアカウントには作り直しが必要な属性が無い。
func (sa *ServiceAccount) RecreateKey() string { return "" }

// GrantPaths は resource.Grants の実装。
// ロールとメンバーは要素単位で比較し、外す操作は vacuum で行う。
func (sa *ServiceAccount) GrantPaths() []string {
	return []string{"project_roles", "iam_policy.*"}
}

// GetProject は resource.File の実装。
func (f *File) GetProject() string { return f.Project }

// SetProject は resource.File の実装。
func (f *File) SetProject(project string) { f.Project = project }

// GetLocation は resource.File の実装。IAM はロケーションを持たない。
func (f *File) GetLocation() string { return "" }

// SetLocation は resource.File の実装。IAM はロケーションを持たないので何もしない。
func (f *File) SetLocation(string) {}

// GetIgnoreChange は resource.File の実装。
func (f *File) GetIgnoreChange() []string { return f.IgnoreChange }

// SetIgnoreChange は resource.File の実装。
func (f *File) SetIgnoreChange(paths []string) { f.IgnoreChange = paths }

// GetItems は resource.File の実装。
func (f *File) GetItems() []*ServiceAccount { return f.ServiceAccounts }

// SetItems は resource.File の実装。
func (f *File) SetItems(sas []*ServiceAccount) { f.ServiceAccounts = sas }

// Sort は resource.File の実装。
func (f *File) Sort() { f.SortServiceAccounts() }
