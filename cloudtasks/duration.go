package cloudtasks

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
)

// formatDuration は API の duration を yaml に書く文字列へ変換する。
// time.Duration に収まる範囲なら Go の duration 表記 ("1h0m0s") を使い、
// 収まらない場合は API と同じ秒表記 ("315576000000.999999999s") をそのまま使う。
// queue.yaml 由来のキューは task_ttl に約 1 万年が入っており、
// time.Duration 経由だと int64 の上限で飽和して別の値になってしまう。
func formatDuration(d *durationpb.Duration) string {
	if d == nil {
		return ""
	}
	if v, ok := goDuration(d); ok {
		return v.String()
	}
	return secondsString(d)
}

// parseDuration は yaml の文字列を API の duration へ変換する。
// Go の duration 表記と秒表記のどちらも受け付ける。
func parseDuration(field, v string) (*durationpb.Duration, error) {
	if v == "" {
		return nil, nil
	}
	if d, err := time.ParseDuration(v); err == nil {
		return durationpb.New(d), nil
	}
	if d, ok := parseSeconds(v); ok {
		return d, nil
	}
	return nil, fmt.Errorf("%s: %q is not a valid duration (e.g. \"1h0m0s\", \"3600s\")", field, v)
}

// goDuration は durationpb を time.Duration へ変換する。
// int64 のナノ秒に収まらない場合は ok が false になる。
func goDuration(d *durationpb.Duration) (time.Duration, bool) {
	secs, nanos := d.GetSeconds(), int64(d.GetNanos())
	if secs > math.MaxInt64/int64(time.Second) || secs < math.MinInt64/int64(time.Second) {
		return 0, false
	}
	total := secs * int64(time.Second)
	if (nanos > 0 && total > math.MaxInt64-nanos) || (nanos < 0 && total < math.MinInt64-nanos) {
		return 0, false
	}
	return time.Duration(total + nanos), true
}

// secondsString は durationpb を "123.456s" 形式の秒表記にする。
func secondsString(d *durationpb.Duration) string {
	nanos := int64(d.GetNanos())
	if nanos == 0 {
		return fmt.Sprintf("%ds", d.GetSeconds())
	}
	if nanos < 0 {
		nanos = -nanos
	}
	frac := strings.TrimRight(fmt.Sprintf("%09d", nanos), "0")
	return fmt.Sprintf("%d.%ss", d.GetSeconds(), frac)
}

// parseSeconds は "315576000000.999999999s" のような秒表記を読む。
func parseSeconds(v string) (*durationpb.Duration, bool) {
	s, ok := strings.CutSuffix(v, "s")
	if !ok || s == "" {
		return nil, false
	}
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	secs, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return nil, false
	}
	d := &durationpb.Duration{Seconds: secs}
	if hasFrac {
		if len(fracPart) == 0 || len(fracPart) > 9 {
			return nil, false
		}
		nanos, err := strconv.ParseInt(fracPart+strings.Repeat("0", 9-len(fracPart)), 10, 32)
		if err != nil {
			return nil, false
		}
		if secs < 0 {
			nanos = -nanos
		}
		d.Nanos = int32(nanos)
	}
	return d, true
}
