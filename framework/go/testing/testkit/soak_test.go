package testkit

import (
	"errors"
	"testing"
)

func TestRunSoakCountsFailuresAndStopsAfterLimit(t *testing.T) {
	calls := 0
	result, err := RunSoak(5, func(i int) error {
		calls++
		if i == 2 {
			return errors.New("boom")
		}
		return nil
	})
	if err == nil || result.Iterations != 5 || result.Failures != 1 || calls != 5 {
		t.Fatalf("soak 结果错误: %#v calls=%d err=%v", result, calls, err)
	}
}

func TestRunSoakRejectsInvalidIterations(t *testing.T) {
	if _, err := RunSoak(0, func(int) error { return nil }); err == nil {
		t.Fatal("iterations=0 必须拒绝")
	}
}
