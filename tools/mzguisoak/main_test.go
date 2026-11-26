package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunRejectsInvalidDuration(t *testing.T) {
	if code := run([]string{"--duration", "-1s"}, os.Stdout, os.Stderr); code == 0 {
		t.Fatal("负时长必须拒绝")
	}
}

func TestRunWritesResourceReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "soak.json")
	if code := run([]string{"--duration", "20ms", "--report", path}, os.Stdout, os.Stderr); code != 0 {
		t.Fatal("短时 soak 应通过")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report soakReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.TasksCreated < 1 || report.OpenConnections != 0 || report.GoroutineDelta > 4 || report.HeapAllocBytes == 0 || report.Duration <= 0 {
		t.Fatalf("报告异常: %#v", report)
	}
}
