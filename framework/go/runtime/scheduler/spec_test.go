package scheduler

import (
	"errors"
	"testing"
	"time"
)

func TestOnceSpecFiresOnlyOnce(t *testing.T) {
	at := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	spec, err := ParseSpec(KindOnce, at.Format(time.RFC3339), "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if next := spec.Next(at.Add(-time.Nanosecond)); !next.Equal(at) {
		t.Fatalf("once next = %s", next)
	}
	if next := spec.Next(at); !next.IsZero() {
		t.Fatalf("once 重复触发 = %s", next)
	}
}

func TestIntervalSpecUsesExplicitAnchorWithoutDrift(t *testing.T) {
	anchor := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	spec, err := ParseSpec(KindInterval, anchor.Format(time.RFC3339)+"/15m", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if next := spec.Next(anchor.Add(46 * time.Minute)); !next.Equal(anchor.Add(time.Hour)) {
		t.Fatalf("interval next = %s", next)
	}
	if next := spec.Next(anchor.Add(365*24*time.Hour + 7*time.Minute)); !next.Equal(anchor.Add(365*24*time.Hour + 15*time.Minute)) {
		t.Fatalf("长时间 interval 漂移: %s", next)
	}
}

func TestCronSpecUsesIANATimezoneAndSkipsMissingDSTTime(t *testing.T) {
	spec, err := ParseSpec(KindCron, "30 2 * * *", "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 3, 7, 8, 0, 0, 0, time.UTC)
	next := spec.Next(start)
	want := time.Date(2026, 3, 9, 6, 30, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("DST 缺失时间 next=%s want=%s", next, want)
	}
}

func TestCronSpecRepeatedDSTTimesRemainDistinctUTCInstants(t *testing.T) {
	spec, err := ParseSpec(KindCron, "30 1 * * *", "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	first := spec.Next(start)
	second := spec.Next(first)
	if first.IsZero() || second.IsZero() || !second.After(first) {
		t.Fatalf("DST 重复时间不单调: first=%s second=%s", first, second)
	}
}

func TestParseSpecRejectsAmbiguousOrUnsafeExpressions(t *testing.T) {
	tests := []struct {
		kind       Kind
		expression string
		timezone   string
	}{
		{KindOnce, "later", "UTC"},
		{KindInterval, "15m", "UTC"},
		{KindInterval, "2026-08-12T00:00:00Z/0s", "UTC"},
		{KindCron, "* * * * * *", "UTC"},
		{KindCron, "* * * * *", "GMT+8"},
		{"unknown", "* * * * *", "UTC"},
	}
	for _, test := range tests {
		if _, err := ParseSpec(test.kind, test.expression, test.timezone); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("非法 spec 未拒绝: %#v err=%v", test, err)
		}
	}
}
