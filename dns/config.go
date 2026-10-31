// Package dns は Cloud DNS のレコードを yaml で管理するための設定ファイルの読み書きと、
// Google Cloud との差分計算を提供する。
//
// 管理するのは yaml に書いたマネージドゾーンの中のレコードだけで、ゾーン自体の作成・削除はしない。
// ゾーン頂点の NS / SOA は Cloud DNS がゾーンと一緒に作るものなので管理対象にしない。
package dns

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultFileName は dns リソースの既定の設定ファイル名。
const DefaultFileName = "dns.yaml"

// File は dns.yaml 1 ファイル分の内容。
// Cloud DNS はロケーションを持たないので location は無い。
type File struct {
	Project string `yaml:"project,omitempty"`
	// IgnoreChange は全レコードに適用される、変更を無視するプロパティパス。
	IgnoreChange []string       `yaml:"ignore_change,omitempty"`
	ManagedZones []*ManagedZone `yaml:"managed_zones"`
}

// ManagedZone はマネージドゾーン 1 件と、その中で管理するレコード。
type ManagedZone struct {
	// Name はゾーン名 (gcloud dns managed-zones list の NAME)。DNS 名ではない。
	Name       string       `yaml:"name"`
	RecordSets []*RecordSet `yaml:"record_sets,omitempty"`
}

// RecordSet はレコードセット 1 件。名前と種類の組で一意になる。
type RecordSet struct {
	// Name は末尾が . の完全修飾ドメイン名 (例: www.example.com.)。
	Name string `yaml:"name"`
	// Type は A / AAAA / CNAME / MX / TXT などのレコードの種類。
	Type string `yaml:"type"`
	TTL  int64  `yaml:"ttl"`
	// RRDatas はレコードの値。Cloud DNS は書いた順に保持するので、並び順も比較する。
	RRDatas []string `yaml:"rrdatas"`
	// IgnoreChange はこのレコードにだけ追加で適用される無視プロパティパス。
	IgnoreChange []string `yaml:"ignore_change,omitempty"`
	// Zone は所属するマネージドゾーン名。yaml ではゾーンの下に書くので書かず、
	// GetItems がゾーンから埋める。差分計算で yaml を経由してコピーされるので yaml タグを持つ。
	Zone string `yaml:"zone,omitempty"`
}

var (
	// zoneNamePattern はマネージドゾーン名の書式。
	zoneNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	// typePattern はレコードの種類の書式。
	typePattern = regexp.MustCompile(`^[A-Z0-9]+$`)
)

// LoadFile は設定ファイルを読み込む。ファイルが無い場合は空の File を返す。
func LoadFile(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &File{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dns: read %s: %w", path, err)
	}
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("dns: parse %s: %w", path, err)
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("dns: %s: %w", path, err)
	}
	return &f, nil
}

// Validate は設定ファイルの内容を検証する。
func (f *File) Validate() error {
	seenZone := map[string]struct{}{}
	for i, z := range f.ManagedZones {
		if z == nil {
			return fmt.Errorf("managed_zones[%d] is empty", i)
		}
		if !zoneNamePattern.MatchString(z.Name) {
			return fmt.Errorf("managed_zones[%d]: name %q must be 1-63 characters of lowercase letters, digits "+
				"and hyphens, starting with a letter", i, z.Name)
		}
		if _, ok := seenZone[z.Name]; ok {
			return fmt.Errorf("managed zone %q is defined more than once", z.Name)
		}
		seenZone[z.Name] = struct{}{}

		seen := map[string]struct{}{}
		for k, rs := range z.RecordSets {
			if rs == nil {
				return fmt.Errorf("managed zone %q: record_sets[%d] is empty", z.Name, k)
			}
			if rs.Zone != "" {
				return fmt.Errorf("managed zone %q: record_sets[%d]: zone は書かないでください。"+
					"レコードは managed_zones の下に書きます", z.Name, k)
			}
			if err := rs.Validate(); err != nil {
				return fmt.Errorf("managed zone %q: record_sets[%d] (%s %s): %w", z.Name, k, rs.Name, rs.Type, err)
			}
			key := rs.Name + " " + rs.Type
			if _, ok := seen[key]; ok {
				return fmt.Errorf("managed zone %q: record %s is defined more than once", z.Name, key)
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

// Validate はレコード 1 件の内容を検証する。
func (rs *RecordSet) Validate() error {
	if rs.Name == "" {
		return fmt.Errorf("name is empty")
	}
	if !strings.HasSuffix(rs.Name, ".") {
		return fmt.Errorf("name は末尾を . にした完全修飾ドメイン名で書いてください (%s.)", rs.Name)
	}
	if rs.Name != strings.ToLower(rs.Name) {
		return fmt.Errorf("name は小文字で書いてください (%s)", strings.ToLower(rs.Name))
	}
	if !typePattern.MatchString(rs.Type) {
		return fmt.Errorf("type %q must be an uppercase record type such as A or CNAME", rs.Type)
	}
	if rs.Type == "SOA" {
		return fmt.Errorf("SOA はゾーンと一緒に Cloud DNS が管理するので書けません")
	}
	if rs.TTL <= 0 {
		return fmt.Errorf("ttl must be greater than 0")
	}
	if len(rs.RRDatas) == 0 {
		return fmt.Errorf("rrdatas is empty")
	}
	return nil
}

// itemName は差分の表示や削除に使う、ゾーンをまたいで一意な名前を返す。
// 書式は "<zone>/<name> <type>"。
func itemName(zone, name, typ string) string {
	return zone + "/" + name + " " + typ
}

// parseItemName は itemName の逆。
func parseItemName(s string) (zone, name, typ string, err error) {
	zone, rest, ok := strings.Cut(s, "/")
	if !ok {
		return "", "", "", fmt.Errorf("dns: invalid record name %q", s)
	}
	name, typ, ok = strings.Cut(rest, " ")
	if !ok || zone == "" || name == "" || typ == "" {
		return "", "", "", fmt.Errorf("dns: invalid record name %q", s)
	}
	return zone, name, typ, nil
}

// lessRecordSet はレコードの並び順。ゾーン名、ドメイン名の階層 (右のラベルから)、種類の順。
// ゾーン頂点が先頭に来て、サブドメインがその下にまとまる。
func lessRecordSet(a, b *RecordSet) bool {
	if a.Zone != b.Zone {
		return a.Zone < b.Zone
	}
	if c := slices.Compare(reversedLabels(a.Name), reversedLabels(b.Name)); c != 0 {
		return c < 0
	}
	return a.Type < b.Type
}

func reversedLabels(name string) []string {
	labels := strings.Split(strings.TrimSuffix(name, "."), ".")
	slices.Reverse(labels)
	return labels
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
			return fmt.Errorf("dns: mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("dns: write %s: %w", path, err)
	}
	return nil
}

// Marshal は設定ファイルを yaml へエンコードする。
// レコードの zone はゾーンの下に書くことで表すので書き出さない。
func (f *File) Marshal() ([]byte, error) {
	out := *f
	out.ManagedZones = make([]*ManagedZone, 0, len(f.ManagedZones))
	for _, z := range f.ManagedZones {
		zc := &ManagedZone{Name: z.Name}
		for _, rs := range z.RecordSets {
			c := *rs
			c.Zone = ""
			zc.RecordSets = append(zc.RecordSets, &c)
		}
		out.ManagedZones = append(out.ManagedZones, zc)
	}

	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(&out); err != nil {
		return nil, fmt.Errorf("dns: encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("dns: encode yaml: %w", err)
	}
	return []byte(sb.String()), nil
}

// ItemName は resource.Item の実装。
func (rs *RecordSet) ItemName() string { return itemName(rs.Zone, rs.Name, rs.Type) }

// ItemIgnoreChange は resource.Item の実装。
func (rs *RecordSet) ItemIgnoreChange() []string { return rs.IgnoreChange }

// SetItemIgnoreChange は resource.Item の実装。
func (rs *RecordSet) SetItemIgnoreChange(paths []string) { rs.IgnoreChange = paths }

// Canonicalize は resource.Item の実装。レコードには同義表現が無い。
func (rs *RecordSet) Canonicalize() error { return nil }

// RecreateKey は resource.Item の実装。名前や種類が変われば別のレコードになるので、
// 作り直しが必要な属性は無い。
func (rs *RecordSet) RecreateKey() string { return "" }

// GetProject は resource.File の実装。
func (f *File) GetProject() string { return f.Project }

// SetProject は resource.File の実装。
func (f *File) SetProject(project string) { f.Project = project }

// GetLocation は resource.File の実装。Cloud DNS はロケーションを持たない。
func (f *File) GetLocation() string { return "" }

// SetLocation は resource.File の実装。Cloud DNS はロケーションを持たないので何もしない。
func (f *File) SetLocation(string) {}

// GetIgnoreChange は resource.File の実装。
func (f *File) GetIgnoreChange() []string { return f.IgnoreChange }

// SetIgnoreChange は resource.File の実装。
func (f *File) SetIgnoreChange(paths []string) { f.IgnoreChange = paths }

// GetItems は resource.File の実装。全ゾーンのレコードを、所属ゾーンを埋めて返す。
func (f *File) GetItems() []*RecordSet {
	var items []*RecordSet
	for _, z := range f.ManagedZones {
		for _, rs := range z.RecordSets {
			rs.Zone = z.Name
			items = append(items, rs)
		}
	}
	return items
}

// SetItems は resource.File の実装。レコードを所属ゾーンごとにまとめ直す。
func (f *File) SetItems(items []*RecordSet) {
	f.ManagedZones = nil
	byZone := map[string]*ManagedZone{}
	for _, rs := range items {
		z, ok := byZone[rs.Zone]
		if !ok {
			z = &ManagedZone{Name: rs.Zone}
			byZone[rs.Zone] = z
			f.ManagedZones = append(f.ManagedZones, z)
		}
		z.RecordSets = append(z.RecordSets, rs)
	}
}

// Sort は resource.File の実装。ゾーンを名前順に、レコードを lessRecordSet の順に並べ替える。
func (f *File) Sort() {
	sort.Slice(f.ManagedZones, func(i, k int) bool { return f.ManagedZones[i].Name < f.ManagedZones[k].Name })
	for _, z := range f.ManagedZones {
		for _, rs := range z.RecordSets {
			rs.Zone = z.Name
		}
		sort.SliceStable(z.RecordSets, func(i, k int) bool { return lessRecordSet(z.RecordSets[i], z.RecordSets[k]) })
	}
}

// ScopeNames は resource.ScopeProvider の実装。管理対象は yaml に書いたゾーン。
func (f *File) ScopeNames() []string {
	names := make([]string, 0, len(f.ManagedZones))
	for _, z := range f.ManagedZones {
		names = append(names, z.Name)
	}
	return names
}
