package cloudtasks_test

import (
	"reflect"
	"testing"

	cloudtaskspb "cloud.google.com/go/cloudtasks/apiv2beta3/cloudtaskspb"
	"github.com/sinmetalcraft/bizmac/cloudtasks"
)

func f64(v float64) *float64 { return &v }
func i32(v int32) *int32     { return &v }
func i64(v int64) *int64     { return &v }

func TestQueueToProtoAndBack(t *testing.T) {
	want := &cloudtasks.Queue{
		Name: "process-nft",
		RateLimits: &cloudtasks.RateLimits{
			MaxDispatchesPerSecond:  f64(500),
			MaxConcurrentDispatches: i32(1),
		},
		RetryConfig: &cloudtasks.RetryConfig{
			MaxAttempts:  i32(-1),
			MinBackoff:   "100ms",
			MaxBackoff:   "1h0m0s",
			MaxDoublings: i32(16),
		},
		TaskTTL:                  "744h0m0s",
		TombstoneTTL:             "1h0m0s",
		StackdriverLoggingConfig: &cloudtasks.StackdriverLoggingConfig{SamplingRatio: f64(1)},
		HTTPTarget: &cloudtasks.HTTPTarget{
			HTTPMethod: "POST",
			URIOverride: &cloudtasks.URIOverride{
				Scheme:                 "HTTPS",
				Host:                   "api.example.com",
				Port:                   i64(443),
				PathOverride:           "/v1/task",
				QueryOverride:          "a=1&b=2",
				URIOverrideEnforceMode: "ALWAYS",
			},
			HeaderOverrides: map[string]string{"X-Origin": "bizmac"},
			OIDCToken: &cloudtasks.OIDCToken{
				ServiceAccountEmail: "tasks@my-project.iam.gserviceaccount.com",
				Audience:            "https://api.example.com",
			},
		},
	}

	pb, err := want.ToProto("my-project", "asia-northeast1")
	if err != nil {
		t.Fatalf("ToProto() error: %v", err)
	}
	if got, wantName := pb.GetName(), "projects/my-project/locations/asia-northeast1/queues/process-nft"; got != wantName {
		t.Errorf("Name = %q, want %q", got, wantName)
	}
	if pb.GetHttpTarget().GetHttpMethod() != cloudtaskspb.HttpMethod_POST {
		t.Errorf("http_method = %v, want POST", pb.GetHttpTarget().GetHttpMethod())
	}
	if got := pb.GetHttpTarget().GetUriOverride().GetPathOverride().GetPath(); got != "/v1/task" {
		t.Errorf("path_override = %q, want /v1/task", got)
	}

	got := cloudtasks.QueueFromProto(pb)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("QueueFromProto(ToProto(queue)) = %#v, want %#v", got, want)
	}
}

func TestQueueToProtoAndBack_appEngine(t *testing.T) {
	// queue.yaml 由来のキュー。task_ttl は time.Duration に収まらない値が入る。
	want := &cloudtasks.Queue{
		Name:         "backup-datastore",
		TaskTTL:      "315576000000.999999999s",
		TombstoneTTL: "216h0m0s",
		AppEngineHTTPQueue: &cloudtasks.AppEngineHTTPQueue{
			AppEngineRoutingOverride: &cloudtasks.AppEngineRouting{
				Host: "ah-builtin-python-bundle.my-project.appspot.com",
			},
		},
	}

	pb, err := want.ToProto("my-project", "asia-northeast1")
	if err != nil {
		t.Fatalf("ToProto() error: %v", err)
	}
	if got := pb.GetTaskTtl(); got.GetSeconds() != 315576000000 || got.GetNanos() != 999999999 {
		t.Errorf("task_ttl = %v, want seconds:315576000000 nanos:999999999", got)
	}
	if got := cloudtasks.QueueFromProto(pb); !reflect.DeepEqual(got, want) {
		t.Errorf("QueueFromProto(ToProto(queue)) = %#v, want %#v", got, want)
	}
}

func TestQueueFromProto_dropsOutputOnlyFields(t *testing.T) {
	// state / purge_time / max_burst_size / type は yaml では扱わない。
	pb := &cloudtaskspb.Queue{
		Name:  "projects/p/locations/l/queues/default",
		State: cloudtaskspb.Queue_PAUSED,
		Type:  cloudtaskspb.Queue_PUSH,
		RateLimits: &cloudtaskspb.RateLimits{
			MaxDispatchesPerSecond:  5,
			MaxBurstSize:            10,
			MaxConcurrentDispatches: 0,
		},
	}
	got := cloudtasks.QueueFromProto(pb)
	if got.Name != "default" {
		t.Errorf("Name = %q, want default", got.Name)
	}
	if got.RateLimits == nil || *got.RateLimits.MaxDispatchesPerSecond != 5 {
		t.Fatalf("rate_limits = %#v", got.RateLimits)
	}
	// 0 は「無制限」という意味のある値なので、未指定と区別できるよう保持する。
	if got.RateLimits.MaxConcurrentDispatches == nil || *got.RateLimits.MaxConcurrentDispatches != 0 {
		t.Errorf("max_concurrent_dispatches = %v, want 0", got.RateLimits.MaxConcurrentDispatches)
	}

	b, err := (&cloudtasks.File{Queues: []*cloudtasks.Queue{got}}).Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	for _, ng := range []string{"state", "purge_time", "max_burst_size", "type"} {
		if contains(string(b), ng) {
			t.Errorf("yaml に %q が含まれています:\n%s", ng, b)
		}
	}
}

func TestQueueToProto_invalid(t *testing.T) {
	cases := map[string]*cloudtasks.Queue{
		"scheme が不正": {Name: "q", HTTPTarget: &cloudtasks.HTTPTarget{
			URIOverride: &cloudtasks.URIOverride{Scheme: "ftp"},
		}},
		"http_method が不正": {Name: "q", HTTPTarget: &cloudtasks.HTTPTarget{HTTPMethod: "FETCH"}},
		"duration が不正":    {Name: "q", TaskTTL: "31 days"},
		"認証が両方":           {Name: "q", HTTPTarget: &cloudtasks.HTTPTarget{OAuthToken: &cloudtasks.OAuthToken{}, OIDCToken: &cloudtasks.OIDCToken{}}},
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := q.ToProto("p", "l"); err == nil {
				t.Error("ToProto() error = nil, want error")
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
