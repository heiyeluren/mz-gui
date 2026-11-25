package testkit

import (
	"errors"
	"testing"
)

func TestRunNightMatrixInjectsFaultsAndAggregatesFailures(t *testing.T) {
	result, err := RunNightMatrix([]MatrixCase{
		{Name: "healthy", Run: func(*FaultInjector) error { return nil }},
		{Name: "disk-full", Run: func(injector *FaultInjector) error {
			injector.Fail("disk.write", 1, errors.New("disk full"))
			return injector.Check("disk.write")
		}},
	})
	if err == nil || result.Cases != 2 || result.Passed != 1 || len(result.Failures) != 1 {
		t.Fatalf("夜间矩阵结果错误: %#v err=%v", result, err)
	}
	if result.Failures[0] != "disk-full: disk full" {
		t.Fatalf("失败摘要错误: %#v", result.Failures)
	}
}

func TestRunNightMatrixRejectsInvalidOrDuplicateCases(t *testing.T) {
	if _, err := RunNightMatrix(nil); err == nil {
		t.Fatal("空矩阵必须拒绝")
	}
	if _, err := RunNightMatrix([]MatrixCase{{Name: "same", Run: func(*FaultInjector) error { return nil }}, {Name: "same", Run: func(*FaultInjector) error { return nil }}}); err == nil {
		t.Fatal("重复用例必须拒绝")
	}
}

func TestRunNightMatrixPassesIndependentCases(t *testing.T) {
	result, err := RunNightMatrix([]MatrixCase{
		{Name: "network", Run: func(injector *FaultInjector) error { return injector.Check("network") }},
		{Name: "sqlite", Run: func(injector *FaultInjector) error { return injector.Check("sqlite") }},
	})
	if err != nil || result.Passed != 2 || result.Cases != 2 {
		t.Fatalf("独立用例应通过: %#v err=%v", result, err)
	}
}
