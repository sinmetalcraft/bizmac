package dns_test

import (
	"reflect"
	"testing"

	"github.com/sinmetalcraft/bizmac/dns"
)

func TestBuildPlan(t *testing.T) {
	file := &dns.File{
		Project: "p",
		ManagedZones: []*dns.ManagedZone{{
			Name: "example",
			RecordSets: []*dns.RecordSet{
				{Name: "example.com.", Type: "A", TTL: 300, RRDatas: []string{"192.0.2.1"}},
				{Name: "www.example.com.", Type: "CNAME", TTL: 60, RRDatas: []string{"example.com."}},
				{Name: "new.example.com.", Type: "A", TTL: 300, RRDatas: []string{"192.0.2.9"}},
			},
		}},
	}
	actual := []*dns.RecordSet{
		{Zone: "example", Name: "example.com.", Type: "A", TTL: 300, RRDatas: []string{"192.0.2.1"}},
		{Zone: "example", Name: "www.example.com.", Type: "CNAME", TTL: 300, RRDatas: []string{"example.com."}},
		{Zone: "example", Name: "old.example.com.", Type: "A", TTL: 300, RRDatas: []string{"192.0.2.8"}},
	}

	plan, err := dns.BuildPlan(file, actual)
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Create) != 1 || plan.Create[0].ItemName() != "example/new.example.com. A" {
		t.Errorf("Create = %v, want [example/new.example.com. A]", plan.Create)
	}
	// 作るレコードは所属ゾーンを持っている (Create で API に渡すため)。
	if len(plan.Create) == 1 && plan.Create[0].Zone != "example" {
		t.Errorf("Create[0].Zone = %q, want example", plan.Create[0].Zone)
	}
	if len(plan.Update) != 1 {
		t.Fatalf("Update = %d 件, want 1 件", len(plan.Update))
	}
	var changes []string
	for _, c := range plan.Update[0].Changes {
		changes = append(changes, c.String())
	}
	if want := []string{"~ ttl: 300 => 60"}; !reflect.DeepEqual(changes, want) {
		t.Errorf("Update[0].Changes = %v, want %v", changes, want)
	}
	if len(plan.Delete) != 1 || plan.Delete[0].ItemName() != "example/old.example.com. A" {
		t.Errorf("Delete = %v, want [example/old.example.com. A]", plan.Delete)
	}
	if len(plan.NoChange) != 1 {
		t.Errorf("NoChange = %d 件, want 1 件", len(plan.NoChange))
	}
}

func TestBuildPlan_rrdatasOrder(t *testing.T) {
	// Cloud DNS は rrdatas を書いた順に保持するので、並び順の違いも差分になる。
	file := &dns.File{
		Project: "p",
		ManagedZones: []*dns.ManagedZone{{
			Name: "example",
			RecordSets: []*dns.RecordSet{
				{Name: "example.com.", Type: "A", TTL: 300, RRDatas: []string{"192.0.2.2", "192.0.2.1"}},
			},
		}},
	}
	actual := []*dns.RecordSet{
		{Zone: "example", Name: "example.com.", Type: "A", TTL: 300, RRDatas: []string{"192.0.2.1", "192.0.2.2"}},
	}
	plan, err := dns.BuildPlan(file, actual)
	if err != nil {
		t.Fatalf("BuildPlan() error: %v", err)
	}
	if len(plan.Update) != 1 {
		t.Errorf("Update = %d 件, want 1 件", len(plan.Update))
	}
}
