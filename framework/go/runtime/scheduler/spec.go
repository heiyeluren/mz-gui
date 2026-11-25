package scheduler

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
)

const (
	minimumInterval = time.Second
	maximumInterval = 365 * 24 * time.Hour
)

type Spec interface {
	Next(time.Time) time.Time
}

func ParseSpec(kind Kind, expression, timezone string) (Spec, error) {
	if expression == "" || len(expression) > 512 || timezone == "" || len(timezone) > 128 {
		return nil, fmt.Errorf("%w: 调度表达式无效", ErrInvalidArgument)
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: timezone 无效", ErrInvalidArgument)
	}
	switch kind {
	case KindOnce:
		value, parseErr := time.Parse(time.RFC3339, expression)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: once 表达式无效", ErrInvalidArgument)
		}
		return onceSpec{at: value.UTC()}, nil
	case KindInterval:
		separator := strings.LastIndex(expression, "/")
		if separator < 1 || separator == len(expression)-1 {
			return nil, fmt.Errorf("%w: interval 必须包含锚点和时长", ErrInvalidArgument)
		}
		anchor, parseErr := time.Parse(time.RFC3339, expression[:separator])
		if parseErr != nil {
			return nil, fmt.Errorf("%w: interval 锚点无效", ErrInvalidArgument)
		}
		duration, parseErr := time.ParseDuration(expression[separator+1:])
		if parseErr != nil || duration < minimumInterval || duration > maximumInterval {
			return nil, fmt.Errorf("%w: interval 时长无效", ErrInvalidArgument)
		}
		return intervalSpec{anchor: anchor.UTC(), duration: duration}, nil
	case KindCron:
		if len(strings.Fields(expression)) != 5 {
			return nil, fmt.Errorf("%w: cron 必须是 5 段", ErrInvalidArgument)
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		parsed, parseErr := parser.Parse(expression)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: cron 表达式无效", ErrInvalidArgument)
		}
		return cronSpec{schedule: parsed, location: location}, nil
	default:
		return nil, fmt.Errorf("%w: schedule kind 无效", ErrInvalidArgument)
	}
}

type onceSpec struct {
	at time.Time
}

func (spec onceSpec) Next(after time.Time) time.Time {
	if after.Before(spec.at) {
		return spec.at
	}
	return time.Time{}
}

type intervalSpec struct {
	anchor   time.Time
	duration time.Duration
}

func (spec intervalSpec) Next(after time.Time) time.Time {
	if after.Before(spec.anchor) {
		return spec.anchor
	}
	elapsed := after.Sub(spec.anchor)
	steps := elapsed/spec.duration + 1
	return spec.anchor.Add(steps * spec.duration)
}

type cronSpec struct {
	schedule cron.Schedule
	location *time.Location
}

func (spec cronSpec) Next(after time.Time) time.Time {
	return spec.schedule.Next(after.In(spec.location)).UTC()
}
