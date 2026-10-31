package dns

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	dnsapi "google.golang.org/api/dns/v1"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "dns.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFile(t *testing.T) {
	p := writeFile(t, `project: p
managed_zones:
  - name: example
    record_sets:
      - name: www.example.com.
        type: A
        ttl: 300
        rrdatas:
          - 192.0.2.1
`)
	f, err := LoadFile(p)
	if err != nil {
		t.Fatalf("LoadFile() error: %v", err)
	}
	items := f.GetItems()
	if len(items) != 1 {
		t.Fatalf("GetItems() = %d 件, want 1 件", len(items))
	}
	if got, want := items[0].ItemName(), "example/www.example.com. A"; got != want {
		t.Errorf("ItemName() = %q, want %q", got, want)
	}
	if got, want := f.ScopeNames(), []string{"example"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ScopeNames() = %v, want %v", got, want)
	}
}

func TestLoadFile_invalid(t *testing.T) {
	tests := []struct {
		name    string
		records string
		wantErr string
	}{
		{
			name:    "末尾の . が無い",
			records: "      - {name: www.example.com, type: A, ttl: 300, rrdatas: [192.0.2.1]}\n",
			wantErr: "完全修飾ドメイン名",
		},
		{
			name:    "大文字の名前",
			records: "      - {name: WWW.example.com., type: A, ttl: 300, rrdatas: [192.0.2.1]}\n",
			wantErr: "小文字",
		},
		{
			name:    "小文字の種類",
			records: "      - {name: www.example.com., type: a, ttl: 300, rrdatas: [192.0.2.1]}\n",
			wantErr: "uppercase",
		},
		{
			name:    "SOA",
			records: "      - {name: example.com., type: SOA, ttl: 300, rrdatas: [x]}\n",
			wantErr: "SOA",
		},
		{
			name:    "ttl が無い",
			records: "      - {name: www.example.com., type: A, rrdatas: [192.0.2.1]}\n",
			wantErr: "ttl",
		},
		{
			name:    "rrdatas が無い",
			records: "      - {name: www.example.com., type: A, ttl: 300}\n",
			wantErr: "rrdatas",
		},
		{
			name:    "zone を書いた",
			records: "      - {name: www.example.com., type: A, ttl: 300, rrdatas: [192.0.2.1], zone: other}\n",
			wantErr: "zone は書かない",
		},
		{
			name: "重複",
			records: "      - {name: www.example.com., type: A, ttl: 300, rrdatas: [192.0.2.1]}\n" +
				"      - {name: www.example.com., type: A, ttl: 60, rrdatas: [192.0.2.2]}\n",
			wantErr: "more than once",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := writeFile(t, "project: p\nmanaged_zones:\n  - name: example\n    record_sets:\n"+tt.records)
			_, err := LoadFile(p)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("LoadFile() error = %v, want %q を含むエラー", err, tt.wantErr)
			}
		})
	}
}

func TestFile_setItemsAndMarshal(t *testing.T) {
	// export は List の結果を SetItems するので、ゾーンごとにまとめ直されて並び、
	// レコードの zone は書き出されない。
	f := &File{Project: "p"}
	f.SetItems([]*RecordSet{
		{Zone: "b-zone", Name: "b.example.net.", Type: "A", TTL: 300, RRDatas: []string{"192.0.2.2"}},
		{Zone: "a-zone", Name: "www.example.com.", Type: "CNAME", TTL: 300, RRDatas: []string{"example.com."}},
		{Zone: "a-zone", Name: "example.com.", Type: "TXT", TTL: 300, RRDatas: []string{`"v=spf1 -all"`}},
		{Zone: "a-zone", Name: "_dmarc.example.com.", Type: "TXT", TTL: 300, RRDatas: []string{`"v=DMARC1"`}},
		{Zone: "a-zone", Name: "example.com.", Type: "A", TTL: 300, RRDatas: []string{"192.0.2.1"}},
	})
	f.Sort()
	b, err := f.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	want := `project: p
managed_zones:
  - name: a-zone
    record_sets:
      - name: example.com.
        type: A
        ttl: 300
        rrdatas:
          - 192.0.2.1
      - name: example.com.
        type: TXT
        ttl: 300
        rrdatas:
          - '"v=spf1 -all"'
      - name: _dmarc.example.com.
        type: TXT
        ttl: 300
        rrdatas:
          - '"v=DMARC1"'
      - name: www.example.com.
        type: CNAME
        ttl: 300
        rrdatas:
          - example.com.
  - name: b-zone
    record_sets:
      - name: b.example.net.
        type: A
        ttl: 300
        rrdatas:
          - 192.0.2.2
`
	if string(b) != want {
		t.Errorf("Marshal() =\n%s\nwant\n%s", b, want)
	}
	// Marshal で zone を落としても、メモリ上のレコードは所属ゾーンを持ったまま。
	if got := f.GetItems()[0].Zone; got != "a-zone" {
		t.Errorf("Zone = %q, want a-zone", got)
	}

	// 書き出したものを読み直せる。
	p := writeFile(t, string(b))
	if _, err := LoadFile(p); err != nil {
		t.Errorf("LoadFile() error: %v", err)
	}
}

func TestParseItemName(t *testing.T) {
	zone, name, typ, err := parseItemName(itemName("example", "www.example.com.", "CNAME"))
	if err != nil {
		t.Fatalf("parseItemName() error: %v", err)
	}
	if zone != "example" || name != "www.example.com." || typ != "CNAME" {
		t.Errorf("parseItemName() = %q, %q, %q", zone, name, typ)
	}
	for _, s := range []string{"www.example.com. A", "example/www.example.com.", "/x A"} {
		if _, _, _, err := parseItemName(s); err == nil {
			t.Errorf("parseItemName(%q) error = nil, want error", s)
		}
	}
}

func TestFromAPI(t *testing.T) {
	rrsets := []*dnsapi.ResourceRecordSet{
		{Name: "example.com.", Type: "A", Ttl: 300, Rrdatas: []string{"192.0.2.1"}},
		{Name: "example.com.", Type: "NS", Ttl: 21600, Rrdatas: []string{"ns-cloud-a1.googledomains.com."}},
		{Name: "example.com.", Type: "SOA", Ttl: 21600, Rrdatas: []string{"ns-cloud-a1.googledomains.com. ..."}},
		// サブドメインの委任の NS は管理対象。
		{Name: "sub.example.com.", Type: "NS", Ttl: 300, Rrdatas: []string{"ns1.example.net."}},
		{Name: "geo.example.com.", Type: "A", Ttl: 300, RoutingPolicy: &dnsapi.RRSetRoutingPolicy{}},
	}
	items, skipped := fromAPI("example", "example.com.", rrsets)
	var names []string
	for _, rs := range items {
		names = append(names, rs.ItemName())
	}
	if want := []string{"example/example.com. A", "example/sub.example.com. NS"}; !reflect.DeepEqual(names, want) {
		t.Errorf("items = %v, want %v", names, want)
	}
	if want := []string{"example/geo.example.com. A"}; !reflect.DeepEqual(skipped, want) {
		t.Errorf("skipped = %v, want %v", skipped, want)
	}
}
