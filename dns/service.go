package dns

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sinmetalcraft/bizmac/prop"
	dnsapi "google.golang.org/api/dns/v1"
)

// Service は Cloud DNS API のクライアント。
type Service struct {
	dns *dnsapi.Service
	// scope は管理対象のゾーン名。scoped が false なら全ゾーンを対象にする。
	scope  []string
	scoped bool
	// notes は直近の List で除外したものなどの注記。
	notes []string
}

// NewService は Application Default Credentials でクライアントを作る。
func NewService(ctx context.Context) (*Service, error) {
	d, err := dnsapi.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("dns: create client: %w", err)
	}
	return &Service{dns: d}, nil
}

// Close は resource.Service の実装。REST クライアントは閉じるものが無い。
func (s *Service) Close() error { return nil }

// Notes は直近の List で除外したものなどの注記を返す。
func (s *Service) Notes() []string { return s.notes }

// SetScope は resource.Scoped の実装。List は names のゾーンのレコードだけを返す。
func (s *Service) SetScope(names []string) {
	s.scoped = true
	s.scope = append([]string(nil), names...)
}

// List は管理対象のゾーンのレコードを返す。location は使わない。
func (s *Service) List(ctx context.Context, project, _ string) ([]*RecordSet, error) {
	s.notes = nil
	dnsNames := map[string]string{}
	var all []string
	err := s.dns.ManagedZones.List(project).Context(ctx).Pages(ctx, func(resp *dnsapi.ManagedZonesListResponse) error {
		for _, z := range resp.ManagedZones {
			dnsNames[z.Name] = z.DnsName
			all = append(all, z.Name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("dns: list managed zones in %s: %w", project, err)
	}

	zones := all
	if s.scoped {
		zones = s.scope
		for _, z := range zones {
			if _, ok := dnsNames[z]; !ok {
				return nil, fmt.Errorf("dns: マネージドゾーン %q が %s にありません。"+
					"bizmac はゾーンを作成しないので、先に gcloud dns managed-zones create で作成してください", z, project)
			}
		}
	}

	var (
		items   []*RecordSet
		skipped []string
	)
	for _, zone := range zones {
		var rrsets []*dnsapi.ResourceRecordSet
		err := s.dns.ResourceRecordSets.List(project, zone).Context(ctx).Pages(ctx, func(resp *dnsapi.ResourceRecordSetsListResponse) error {
			rrsets = append(rrsets, resp.Rrsets...)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("dns: list record sets in %s: %w", zone, err)
		}
		got, sk := fromAPI(zone, dnsNames[zone], rrsets)
		items = append(items, got...)
		skipped = append(skipped, sk...)
	}
	if len(skipped) > 0 {
		s.notes = append(s.notes, fmt.Sprintf("ルーティングポリシーを使うレコード %d 件は管理対象外のため除外しました (%s)",
			len(skipped), strings.Join(skipped, ", ")))
	}
	sort.SliceStable(items, func(i, k int) bool { return lessRecordSet(items[i], items[k]) })
	return items, nil
}

// fromAPI は API のレコードを変換する。ゾーン頂点の NS / SOA は管理対象にしないので落とし、
// ルーティングポリシーを使うレコード (rrdatas を持たない) は skipped として名前を返す。
func fromAPI(zone, dnsName string, rrsets []*dnsapi.ResourceRecordSet) (items []*RecordSet, skipped []string) {
	for _, r := range rrsets {
		if r.Name == dnsName && (r.Type == "NS" || r.Type == "SOA") {
			continue
		}
		if r.RoutingPolicy != nil {
			skipped = append(skipped, itemName(zone, r.Name, r.Type))
			continue
		}
		items = append(items, &RecordSet{
			Zone:    zone,
			Name:    r.Name,
			Type:    r.Type,
			TTL:     r.Ttl,
			RRDatas: r.Rrdatas,
		})
	}
	return items, skipped
}

func toAPI(rs *RecordSet) *dnsapi.ResourceRecordSet {
	return &dnsapi.ResourceRecordSet{
		Name:    rs.Name,
		Type:    rs.Type,
		Ttl:     rs.TTL,
		Rrdatas: rs.RRDatas,
	}
}

// Create はレコードを作る。
func (s *Service) Create(ctx context.Context, project, _ string, rs *RecordSet) error {
	if _, err := s.dns.ResourceRecordSets.Create(project, rs.Zone, toAPI(rs)).Context(ctx).Do(); err != nil {
		return fmt.Errorf("dns: create record %s: %w", rs.ItemName(), err)
	}
	return nil
}

// Update は既存のレコードを rs のとおりにする。変わるのは ttl と rrdatas だけなので、
// changes は使わずにまとめて送る。
func (s *Service) Update(ctx context.Context, project, _ string, rs *RecordSet, _ []prop.Change) error {
	if _, err := s.dns.ResourceRecordSets.Patch(project, rs.Zone, rs.Name, rs.Type, toAPI(rs)).Context(ctx).Do(); err != nil {
		return fmt.Errorf("dns: update record %s: %w", rs.ItemName(), err)
	}
	return nil
}

// Delete はレコードを削除する。name は RecordSet.ItemName の書式。
func (s *Service) Delete(ctx context.Context, project, _, name string) error {
	zone, rrname, typ, err := parseItemName(name)
	if err != nil {
		return err
	}
	if _, err := s.dns.ResourceRecordSets.Delete(project, zone, rrname, typ).Context(ctx).Do(); err != nil {
		return fmt.Errorf("dns: delete record %s: %w", name, err)
	}
	return nil
}
