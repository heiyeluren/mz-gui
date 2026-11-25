package logging

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
)

func TestJSONLoggerRedactsAndRotatesConcurrentWrites(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "agentd")
	logger, closer, err := New(Options{
		Directory:   directory,
		ProcessName: "sample-agent",
		Component:   "sample",
		Level:       "info",
		MaxBytes:    1024,
	})
	if err != nil {
		t.Fatalf("创建日志器: %v", err)
	}

	var group sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for index := 0; index < 25; index++ {
				logger.InfoContext(context.Background(), "并发日志",
					"worker", worker,
					"request_id", fmt.Sprintf("req-%d-%d", worker, index),
					"token", "must-not-appear")
			}
		}(worker)
	}
	group.Wait()
	if err := closer.Close(); err != nil {
		t.Fatalf("关闭日志器: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(directory, "sample-agent-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 2 {
		t.Fatalf("日志必须按大小轮转，实际文件数 %d", len(files))
	}

	lineCount := 0
	for _, name := range files {
		file, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			lineCount++
			var event map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				t.Fatalf("日志不是合法 JSONL: %v", err)
			}
			if event["component"] != "sample" {
				t.Fatalf("缺少 component 字段: %#v", event)
			}
			if event["token"] != "[REDACTED]" {
				t.Fatalf("敏感字段未脱敏: %#v", event["token"])
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	if lineCount != 100 {
		t.Fatalf("日志行数 = %d, want 100", lineCount)
	}
}

func TestLoggerInjectsTraceContextFieldsFromContext(t *testing.T) {
	directory := t.TempDir()
	logger, closer, err := New(Options{
		Directory: directory, ProcessName: "sample-agent", Component: "sample", Level: "info", MaxBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	trace, err := tracecontext.New(true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := tracecontext.WithContext(context.Background(), trace)
	if err != nil {
		t.Fatal(err)
	}
	logger.InfoContext(ctx, "with-trace", "request_id", "req-1")
	logger.InfoContext(context.Background(), "without-trace")
	logger.InfoContext(ctx, "explicit-trace", "trace_id", "explicit-trace-id")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(directory, "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("日志文件错误: files=%v err=%v", files, err)
	}
	events := decodeLogEvents(t, files[0])
	if len(events) != 3 {
		t.Fatalf("日志行数 = %d", len(events))
	}
	if events[0]["trace_id"] != trace.TraceID || events[0]["span_id"] != trace.SpanID {
		t.Fatalf("context trace 未注入: %#v", events[0])
	}
	if _, exists := events[1]["trace_id"]; exists {
		t.Fatalf("无 trace 的日志不得写入空 trace_id: %#v", events[1])
	}
	if events[2]["trace_id"] != "explicit-trace-id" {
		t.Fatalf("显式 trace_id 不得被覆盖: %#v", events[2])
	}
}

func TestLoggerInjectsRequestIDFromModelContext(t *testing.T) {
	directory := t.TempDir()
	logger, closer, err := New(Options{
		Directory: directory, ProcessName: "sample-agent", Component: "sample", Level: "info", MaxBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := model.WithRequestID(context.Background(), "request-ctx")
	logger.InfoContext(ctx, "from-context")
	logger.InfoContext(ctx, "explicit", "request_id", "request-explicit")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(directory, "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("日志文件错误: files=%v err=%v", files, err)
	}
	events := decodeLogEvents(t, files[0])
	if len(events) != 2 {
		t.Fatalf("日志行数 = %d", len(events))
	}
	if events[0]["request_id"] != "request-ctx" {
		t.Fatalf("context request_id 未注入: %#v", events[0])
	}
	if events[1]["request_id"] != "request-explicit" {
		t.Fatalf("显式 request_id 不得被覆盖: %#v", events[1])
	}
}

func TestNewRejectsInvalidOptionsAndAcceptsEveryLevel(t *testing.T) {
	tests := []Options{
		{ProcessName: "sample-agent", Component: "sample", Level: "info", MaxBytes: 1},
		{Directory: t.TempDir(), Component: "sample", Level: "info", MaxBytes: 1},
		{Directory: t.TempDir(), ProcessName: "sample-agent", Level: "info", MaxBytes: 1},
		{Directory: t.TempDir(), ProcessName: "sample-agent", Component: "sample", Level: "info", MaxBytes: 0},
		{Directory: t.TempDir(), ProcessName: "sample-agent", Component: "sample", Level: "trace", MaxBytes: 1},
	}
	for _, options := range tests {
		if _, _, err := New(options); err == nil {
			t.Fatalf("非法日志选项必须拒绝: %#v", options)
		}
	}

	for _, level := range []string{"debug", "info", "warn", "error"} {
		logger, closer, err := New(Options{
			Directory: t.TempDir(), ProcessName: "sample-agent", Component: "sample", Level: level, MaxBytes: 1024,
		})
		if err != nil {
			t.Fatalf("合法日志级别 %s 被拒绝: %v", level, err)
		}
		logger.Log(context.Background(), slog.LevelError, "level check")
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoggerRedactsNestedAndGroupedSensitiveAttributes(t *testing.T) {
	directory := t.TempDir()
	logger, closer, err := New(Options{
		Directory: directory, ProcessName: "sample-agent", Component: "sample", Level: "debug", MaxBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	logger.WithGroup("request").With("api_key", "hidden").Info("nested",
		slog.Group("credential", slog.String("refresh_token", "hidden-too"), slog.String("kind", "oauth")))
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(directory, "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("日志文件错误: files=%v err=%v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || containsAny(string(data), "hidden", "hidden-too") {
		t.Fatalf("嵌套敏感字段泄漏: %s", data)
	}
}

func decodeLogEvents(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("日志不是合法 JSONL: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
