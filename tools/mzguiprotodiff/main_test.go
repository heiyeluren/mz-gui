package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const oldProto = `syntax = "proto3";
package mzgui.task.v1;
message Task {
  string id = 1;
  int32 count = 2;
  string old_name = 3;
  repeated string tags = 4;
  map<string, string> labels = 5;
  message Nested { bool ready = 1; }
  Nested nested = 6;
}
`

func TestCompareProto_CompatibleAdditionAndReservedRemoval(t *testing.T) {
	newProto := `syntax = "proto3";
package mzgui.task.v1;
message Task {
  string id = 1;
  int32 count = 2;
  repeated string tags = 4;
  map<string, string> labels = 5;
  message Nested { bool ready = 1; }
  Nested nested = 6;
  string added = 7;
  reserved 3;
  reserved "old_name";
}
`
	if issues := compareProto(oldProto, newProto); len(issues) != 0 {
		t.Fatalf("兼容变更不应告警: %v", issues)
	}
}

func TestCompareProto_BreakingChanges(t *testing.T) {
	tests := []struct {
		name      string
		newProto  string
		wantIssue string
	}{
		{
			name: "字段删除且未 reserved",
			newProto: `package mzgui.task.v1; message Task {
				string id = 1; int32 count = 2; repeated string tags = 4;
				map<string, string> labels = 5;
				message Nested { bool ready = 1; } Nested nested = 6;
			}`,
			wantIssue: "old_name",
		},
		{
			name: "字段号被新字段复用",
			newProto: `package mzgui.task.v1; message Task {
				string id = 1; int32 count = 2; string renamed = 3; repeated string tags = 4;
				map<string, string> labels = 5;
				message Nested { bool ready = 1; } Nested nested = 6;
			}`,
			wantIssue: "字段号",
		},
		{
			name: "字段类型改变",
			newProto: `package mzgui.task.v1; message Task {
				string id = 1; int64 count = 2; reserved 3; reserved "old_name"; repeated string tags = 4;
				map<string, string> labels = 5;
				message Nested { bool ready = 1; } Nested nested = 6;
			}`,
			wantIssue: "类型",
		},
		{
			name: "repeated label 改变",
			newProto: `package mzgui.task.v1; message Task {
				string id = 1; int32 count = 2; reserved 3; reserved "old_name"; string tags = 4;
				map<string, string> labels = 5;
				message Nested { bool ready = 1; } Nested nested = 6;
			}`,
			wantIssue: "类型",
		},
		{
			name: "reserved 字段号不能复用",
			newProto: `package mzgui.task.v1; message Task {
				string id = 1; int32 count = 2; reserved 3; reserved "old_name"; string renamed = 3; repeated string tags = 4;
				map<string, string> labels = 5;
				message Nested { bool ready = 1; } Nested nested = 6;
			}`,
			wantIssue: "reserved",
		},
		{
			name: "嵌套消息字段删除",
			newProto: `package mzgui.task.v1; message Task {
				string id = 1; int32 count = 2; reserved 3; reserved "old_name"; repeated string tags = 4;
				map<string, string> labels = 5; message Nested {} Nested nested = 6;
			}`,
			wantIssue: "Task.Nested.ready",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			issues := compareProto(oldProto, test.newProto)
			if len(issues) == 0 || !strings.Contains(strings.Join(issues, "\n"), test.wantIssue) {
				t.Fatalf("应报告包含 %q 的破坏变更: %v", test.wantIssue, issues)
			}
		})
	}
}

func TestCompareProto_ReservedRangeAndName(t *testing.T) {
	newProto := `package mzgui.task.v1; message Task {
		string id = 1; int32 count = 2; repeated string tags = 4;
		map<string, string> labels = 5;
		message Nested { bool ready = 1; } Nested nested = 6;
		reserved 3 to 3; reserved "old_name";
	}`
	if issues := compareProto(oldProto, newProto); len(issues) != 0 {
		t.Fatalf("reserved range 应允许字段退役: %v", issues)
	}
}

func TestRun_ComparesProtoDirectories(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	for name, content := range map[string]string{"task.proto": oldProto, "health.proto": "package mzgui.health.v1; message Health { string status = 1; }"} {
		if err := os.WriteFile(filepath.Join(oldDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{"task.proto": oldProto, "health.proto": "package mzgui.health.v1; message Health { string status = 1; string detail = 2; }"} {
		if err := os.WriteFile(filepath.Join(newDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--old", oldDir, "--new", newDir}, &stdout, &stderr); code != 0 {
		t.Fatalf("目录比较应通过: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "breaking=0") {
		t.Fatalf("CLI 应输出零破坏: %s", stdout.String())
	}
}

func TestRun_ReportsBreakingAndInputErrors(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.proto")
	newPath := filepath.Join(dir, "new.proto")
	if err := os.WriteFile(oldPath, []byte(oldProto), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("package mzgui.task.v1; message Task {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--old", oldPath, "--new", newPath}, &stdout, &stderr); code != 2 {
		t.Fatalf("破坏变更应返回 2: code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(nil, &stdout, &stderr); code != 1 {
		t.Fatalf("缺少参数应返回 1: %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--old", filepath.Join(dir, "missing"), "--new", newPath}, &stdout, &stderr); code != 1 {
		t.Fatalf("输入不存在应返回 1: %d", code)
	}
}

func TestCompareProto_SkipsCommentsEnumsAndServices(t *testing.T) {
	oldSource := `package sample; // message Fake { string bad = 90; }
	message Item { string name = 1; enum State { UNKNOWN = 0; } service NotAService { rpc Get(Item) returns (Item); } }
	/* message AlsoFake { int32 nope = 91; } */
	`
	newSource := `package sample; message Item { string name = 1; enum State { UNKNOWN = 0; READY = 1; } }`
	if issues := compareProto(oldSource, newSource); len(issues) != 0 {
		t.Fatalf("注释、enum 和 service 不应被当作消息字段: %v", issues)
	}
}
