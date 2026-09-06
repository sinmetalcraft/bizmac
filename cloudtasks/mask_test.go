package cloudtasks

import (
	"reflect"
	"testing"

	"github.com/sinmetalcraft/bizmac/prop"
)

func TestUpdateMask(t *testing.T) {
	changes := []prop.Change{
		{Path: "rate_limits.max_dispatches_per_second", Type: prop.Modified},
		{Path: "rate_limits.max_concurrent_dispatches", Type: prop.Added},
		{Path: "retry_config.max_attempts", Type: prop.Modified},
		{Path: "task_ttl", Type: prop.Modified},
		{Path: "http_target.uri_override.host", Type: prop.Modified},
		{Path: "http_target.header_overrides.X-Origin", Type: prop.Added},
		{Path: "app_engine_http_queue.app_engine_routing_override.service", Type: prop.Modified},
		{Path: "stackdriver_logging_config.sampling_ratio", Type: prop.Added},
		// name と ignore_change はマスクに入れない。
		{Path: "name", Type: prop.Modified},
		{Path: "ignore_change", Type: prop.Added},
	}
	want := []string{
		"app_engine_http_queue",
		"http_target",
		"rate_limits.max_concurrent_dispatches",
		"rate_limits.max_dispatches_per_second",
		"retry_config.max_attempts",
		"stackdriver_logging_config",
		"task_ttl",
	}
	got := updateMask(changes).GetPaths()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("updateMask() = %v, want %v", got, want)
	}
}

func TestUpdateMask_empty(t *testing.T) {
	if got := updateMask(nil).GetPaths(); len(got) != 0 {
		t.Errorf("updateMask(nil) = %v, want 空", got)
	}
}
