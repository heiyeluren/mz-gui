package scheduler

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

const maximumPlannedOccurrences = 100

func PlanDue(schedule Schedule, now time.Time, misfireGrace time.Duration) (Plan, error) {
	if schedule.ID == "" || !schedule.Enabled || schedule.DeletedAt != nil || schedule.NextRunAt == nil ||
		schedule.MaxCatchUp < 1 || schedule.MaxCatchUp > maximumPlannedOccurrences ||
		schedule.Jitter < 0 || schedule.Jitter > time.Hour || misfireGrace < 0 || now.IsZero() {
		return Plan{}, fmt.Errorf("%w: schedule 计划状态无效", ErrInvalidArgument)
	}
	spec, err := ParseSpec(schedule.Kind, schedule.Expression, schedule.Timezone)
	if err != nil {
		return Plan{}, err
	}
	next := schedule.NextRunAt.UTC()
	if next.After(now) {
		return Plan{NextRunAt: nonZeroPointer(next)}, nil
	}
	overdue := now.Sub(next) > misfireGrace
	plan := Plan{Occurrences: make([]PlannedOccurrence, 0, schedule.MaxCatchUp)}
	appendOccurrence := func(planned time.Time) {
		plan.Occurrences = append(plan.Occurrences, PlannedOccurrence{
			PlannedAt: planned, ScheduledAt: planned.Add(StableJitter(schedule.ID, planned, schedule.Jitter)),
		})
	}

	switch {
	case !overdue:
		appendOccurrence(next)
		plan.NextRunAt = nonZeroPointer(spec.Next(next))
	case schedule.MisfirePolicy == MisfireSkip:
		plan.NextRunAt = nonZeroPointer(spec.Next(now))
	case schedule.MisfirePolicy == MisfireRunOnce:
		appendOccurrence(next)
		plan.NextRunAt = nonZeroPointer(spec.Next(now))
	case schedule.MisfirePolicy == MisfireCatchUpLimited:
		candidate := next
		for len(plan.Occurrences) < schedule.MaxCatchUp && !candidate.IsZero() && !candidate.After(now) {
			appendOccurrence(candidate)
			candidate = spec.Next(candidate)
		}
		if !candidate.IsZero() && !candidate.After(now) {
			candidate = spec.Next(now)
		}
		plan.NextRunAt = nonZeroPointer(candidate)
	default:
		return Plan{}, fmt.Errorf("%w: misfire policy 无效", ErrInvalidArgument)
	}
	return plan, nil
}

func StableJitter(scheduleID string, plannedAt time.Time, maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	digest := sha256.Sum256([]byte(scheduleID + "\x00" + plannedAt.UTC().Format(time.RFC3339Nano)))
	nanoseconds := uint64(maximum.Nanoseconds())
	return time.Duration(binary.BigEndian.Uint64(digest[:8]) % (nanoseconds + 1))
}

func nonZeroPointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}
