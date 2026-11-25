package scheduler

import (
	"errors"
	"testing"
	"time"
)

func TestPlanDueNormalOccurrence(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 5, 0, time.UTC)
	due := now.Add(-5 * time.Second)
	schedule := testSchedule(MisfireSkip, due)
	plan, err := PlanDue(schedule, now, time.Minute)
	if err != nil || len(plan.Occurrences) != 1 || !plan.Occurrences[0].PlannedAt.Equal(due) ||
		plan.NextRunAt == nil || !plan.NextRunAt.Equal(due.Add(15*time.Minute)) {
		t.Fatalf("正常计划错误: plan=%#v err=%v", plan, err)
	}
}

func TestPlanDueAppliesMisfirePoliciesAfterSleep(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	due := now.Add(-time.Hour)
	tests := []struct {
		policy MisfirePolicy
		count  int
	}{
		{MisfireSkip, 0},
		{MisfireRunOnce, 1},
		{MisfireCatchUpLimited, 3},
	}
	for _, test := range tests {
		schedule := testSchedule(test.policy, due)
		schedule.MaxCatchUp = 3
		plan, err := PlanDue(schedule, now, time.Minute)
		if err != nil || len(plan.Occurrences) != test.count || plan.NextRunAt == nil || !plan.NextRunAt.After(now) {
			t.Fatalf("策略 %s 错误: plan=%#v err=%v", test.policy, plan, err)
		}
	}
}

func TestPlanDueCatchUpIsBoundedForLongSleep(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	due := now.Add(-365 * 24 * time.Hour)
	schedule := testSchedule(MisfireCatchUpLimited, due)
	schedule.Expression = due.Format(time.RFC3339) + "/1s"
	schedule.MaxCatchUp = 5
	plan, err := PlanDue(schedule, now, time.Minute)
	if err != nil || len(plan.Occurrences) != 5 || plan.NextRunAt == nil || !plan.NextRunAt.After(now) {
		t.Fatalf("长休眠追赶错误: plan=%#v err=%v", plan, err)
	}
}

func TestStableJitterIsDeterministicAndBounded(t *testing.T) {
	planned := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	first := StableJitter("schedule-1", planned, 30*time.Second)
	second := StableJitter("schedule-1", planned, 30*time.Second)
	if first != second || first < 0 || first > 30*time.Second {
		t.Fatalf("jitter 不稳定或越界: first=%s second=%s", first, second)
	}
	if zero := StableJitter("schedule-1", planned, 0); zero != 0 {
		t.Fatalf("零 jitter = %s", zero)
	}
}

func TestNonZeroPointerAndScheduleTimeRejectZeroValues(t *testing.T) {
	if value := nonZeroPointer(time.Time{}); value != nil {
		t.Fatalf("零时间指针 = %v", value)
	}
	if _, err := parseScheduleTime(""); err == nil {
		t.Fatal("空调度时间必须拒绝")
	}
}

func TestPlanDueRejectsInvalidScheduleState(t *testing.T) {
	now := time.Now().UTC()
	schedule := testSchedule(MisfireSkip, now)
	schedule.NextRunAt = nil
	if _, err := PlanDue(schedule, now, time.Minute); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil next 错误 = %v", err)
	}
	schedule.NextRunAt = &now
	schedule.MaxCatchUp = 0
	if _, err := PlanDue(schedule, now, time.Minute); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法 catch-up 错误 = %v", err)
	}
}

func TestPlanDueKeepsFutureCursorAndZeroPointer(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	schedule := testSchedule(MisfireSkip, future)
	plan, err := PlanDue(schedule, now, time.Minute)
	if err != nil || len(plan.Occurrences) != 0 || plan.NextRunAt == nil || !plan.NextRunAt.Equal(future) {
		t.Fatalf("未来 schedule 计划错误: plan=%#v err=%v", plan, err)
	}
	if value := nonZeroPointer(time.Time{}); value != nil {
		t.Fatalf("零时间必须返回 nil: %#v", value)
	}
	if _, err := parseScheduleTime(""); err == nil {
		t.Fatal("空 schedule 时间必须拒绝")
	}
}

func testSchedule(policy MisfirePolicy, next time.Time) Schedule {
	anchor := next.Add(-time.Hour).Truncate(time.Minute)
	return Schedule{
		ID: "schedule-1", Enabled: true, Kind: KindInterval,
		Expression: anchor.Format(time.RFC3339) + "/15m", Timezone: "UTC",
		MisfirePolicy: policy, OverlapPolicy: OverlapForbid,
		MaxCatchUp: 1, OverlapLimit: 1, NextRunAt: &next,
	}
}
