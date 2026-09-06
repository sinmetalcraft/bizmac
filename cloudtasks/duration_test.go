package cloudtasks

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestFormatDurationAndBack(t *testing.T) {
	cases := map[string]struct {
		pb   *durationpb.Duration
		want string
	}{
		"nil":  {nil, ""},
		"0.1s": {&durationpb.Duration{Nanos: 100000000}, "100ms"},
		"1h":   {&durationpb.Duration{Seconds: 3600}, "1h0m0s"},
		"9日":   {&durationpb.Duration{Seconds: 777600}, "216h0m0s"},
		// queue.yaml 由来のキューが持つ約 1 万年。time.Duration には収まらないので
		// 秒表記のまま扱い、往復しても値が変わらないことを確かめる。
		"1万年": {&durationpb.Duration{Seconds: 315576000000, Nanos: 999999999}, "315576000000.999999999s"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := formatDuration(c.pb)
			if got != c.want {
				t.Fatalf("formatDuration() = %q, want %q", got, c.want)
			}
			back, err := parseDuration("task_ttl", got)
			if err != nil {
				t.Fatalf("parseDuration(%q) error: %v", got, err)
			}
			if !proto.Equal(back, c.pb) {
				t.Errorf("parseDuration(formatDuration(d)) = %v, want %v", back, c.pb)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]*durationpb.Duration{
		"":                nil,
		"0.100s":          {Nanos: 100000000},
		"100ms":           {Nanos: 100000000},
		"3600s":           {Seconds: 3600},
		"1h0m0s":          {Seconds: 3600},
		"-1.5s":           {Seconds: -1, Nanos: -500000000},
		"999999999999.5s": {Seconds: 999999999999, Nanos: 500000000},
	}
	for in, want := range cases {
		got, err := parseDuration("task_ttl", in)
		if err != nil {
			t.Fatalf("parseDuration(%q) error: %v", in, err)
		}
		if !proto.Equal(got, want) {
			t.Errorf("parseDuration(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseDuration_invalid(t *testing.T) {
	for _, in := range []string{"3600", "abc", "1.2.3s", "s"} {
		if _, err := parseDuration("task_ttl", in); err == nil {
			t.Errorf("parseDuration(%q) error = nil, want error", in)
		}
	}
}
