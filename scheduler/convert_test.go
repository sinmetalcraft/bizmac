package scheduler_test

import (
	"reflect"
	"testing"

	schedulerpb "cloud.google.com/go/scheduler/apiv1/schedulerpb"
	"github.com/sinmetalcraft/bizmac/scheduler"
)

func TestJobToProtoAndBack(t *testing.T) {
	want := &scheduler.Job{
		Name:            "nightly-batch",
		Description:     "毎晩のバッチ",
		Schedule:        "0 3 * * *",
		TimeZone:        "Asia/Tokyo",
		AttemptDeadline: "3m0s",
		RetryConfig: &scheduler.RetryConfig{
			RetryCount:         3,
			MinBackoffDuration: "5s",
			MaxDoublings:       2,
		},
		HTTPTarget: &scheduler.HTTPTarget{
			URI:        "https://example.com/batch",
			HTTPMethod: "POST",
			Headers:    map[string]string{"Content-Type": "application/json"},
			BodyJSON:   map[string]any{"kind": "batch"},
			OIDCToken: &scheduler.OIDCToken{
				ServiceAccountEmail: "scheduler@example.iam.gserviceaccount.com",
				Audience:            "https://example.com",
			},
		},
	}

	pb, err := want.ToProto("my-project", "asia-northeast1")
	if err != nil {
		t.Fatalf("ToProto() error: %v", err)
	}
	if got, wantName := pb.GetName(), "projects/my-project/locations/asia-northeast1/jobs/nightly-batch"; got != wantName {
		t.Errorf("Name = %q, want %q", got, wantName)
	}
	if got, wantBody := string(pb.GetHttpTarget().GetBody()), `{"kind":"batch"}`; got != wantBody {
		t.Errorf("body = %q, want %q", got, wantBody)
	}
	if pb.GetHttpTarget().GetHttpMethod() != schedulerpb.HttpMethod_POST {
		t.Errorf("http_method = %v, want POST", pb.GetHttpTarget().GetHttpMethod())
	}

	got := scheduler.JobFromProto(pb)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JobFromProto(ToProto(job)) = %#v, want %#v", got, want)
	}
}

func TestJobToProto_pubsubTarget(t *testing.T) {
	j := &scheduler.Job{
		Name:     "notify",
		Schedule: "*/5 * * * *",
		PubsubTarget: &scheduler.PubsubTarget{
			TopicName:  "projects/my-project/topics/notify",
			Data:       "hello",
			Attributes: map[string]string{"origin": "bizmac"},
		},
	}
	pb, err := j.ToProto("my-project", "asia-northeast1")
	if err != nil {
		t.Fatalf("ToProto() error: %v", err)
	}
	if got := string(pb.GetPubsubTarget().GetData()); got != "hello" {
		t.Errorf("data = %q, want %q", got, "hello")
	}
	if got := scheduler.JobFromProto(pb); !reflect.DeepEqual(got, j) {
		t.Errorf("JobFromProto() = %#v, want %#v", got, j)
	}
}

func TestJobToProto_invalidValues(t *testing.T) {
	cases := []struct {
		name string
		job  *scheduler.Job
	}{
		{
			name: "duration が不正",
			job: &scheduler.Job{
				Name: "a", AttemptDeadline: "3 minutes",
				HTTPTarget: &scheduler.HTTPTarget{URI: "https://example.com"},
			},
		},
		{
			name: "http_method が不正",
			job: &scheduler.Job{
				Name:       "a",
				HTTPTarget: &scheduler.HTTPTarget{URI: "https://example.com", HTTPMethod: "post"},
			},
		},
		{
			name: "ターゲットが無い",
			job:  &scheduler.Job{Name: "a"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.job.ToProto("p", "l"); err == nil {
				t.Error("ToProto() error = nil, want error")
			}
		})
	}
}

func TestJobValidate(t *testing.T) {
	cases := []struct {
		name    string
		job     *scheduler.Job
		wantErr bool
	}{
		{
			name: "ターゲットが 1 つ",
			job:  &scheduler.Job{Name: "a", HTTPTarget: &scheduler.HTTPTarget{URI: "https://example.com"}},
		},
		{
			name:    "ターゲットが 2 つ",
			job:     &scheduler.Job{Name: "a", HTTPTarget: &scheduler.HTTPTarget{}, PubsubTarget: &scheduler.PubsubTarget{}},
			wantErr: true,
		},
		{
			name:    "body と body_json の併用",
			job:     &scheduler.Job{Name: "a", HTTPTarget: &scheduler.HTTPTarget{Body: "x", BodyJSON: map[string]any{}}},
			wantErr: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.job.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
