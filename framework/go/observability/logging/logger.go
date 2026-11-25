package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
)

type Options struct {
	Directory   string
	ProcessName string
	Component   string
	Level       string
	MaxBytes    int64
}

// New 创建每进程独占的 JSONL 日志器。
func New(options Options) (*slog.Logger, io.Closer, error) {
	if options.Directory == "" || options.ProcessName == "" || options.Component == "" {
		return nil, nil, fmt.Errorf("日志目录、进程名和组件名不能为空")
	}
	if options.MaxBytes <= 0 {
		return nil, nil, fmt.Errorf("日志文件大小上限必须为正数")
	}
	level, err := parseLevel(options.Level)
	if err != nil {
		return nil, nil, err
	}
	writer := &rotatingWriter{
		directory:   options.Directory,
		processName: options.ProcessName,
		maxBytes:    options.MaxBytes,
	}
	if err := os.MkdirAll(options.Directory, 0o700); err != nil {
		return nil, nil, fmt.Errorf("创建日志目录: %w", err)
	}
	if err := os.Chmod(options.Directory, 0o700); err != nil {
		return nil, nil, fmt.Errorf("设置日志目录权限: %w", err)
	}

	base := slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})
	handler := redactingHandler{next: base}
	logger := slog.New(handler).With("component", options.Component)
	return logger, writer, nil
}

func parseLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("不支持的日志级别 %q", value)
	}
}

type rotatingWriter struct {
	directory   string
	processName string
	maxBytes    int64

	mu       sync.Mutex
	file     *os.File
	date     string
	sequence int
	size     int64
}

func (writer *rotatingWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	today := time.Now().UTC().Format("20060102")
	if writer.file == nil || writer.date != today || writer.size+int64(len(data)) > writer.maxBytes {
		if err := writer.rotate(today); err != nil {
			return 0, err
		}
	}
	written, err := writer.file.Write(data)
	writer.size += int64(written)
	return written, err
}

func (writer *rotatingWriter) Close() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return nil
	}
	err := writer.file.Close()
	writer.file = nil
	return err
}

func (writer *rotatingWriter) rotate(today string) error {
	if writer.file != nil {
		if err := writer.file.Close(); err != nil {
			return err
		}
		writer.file = nil
	}
	if writer.date != today {
		writer.date = today
		writer.sequence = 0
	}
	for {
		writer.sequence++
		name := filepath.Join(writer.directory,
			fmt.Sprintf("%s-%s-%03d.jsonl", writer.processName, today, writer.sequence))
		file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("创建日志文件: %w", err)
		}
		writer.file = file
		writer.size = 0
		return nil
	}
}

type redactingHandler struct {
	next slog.Handler
}

func (handler redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.next.Enabled(ctx, level)
}

func (handler redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	seen := make(map[string]struct{})
	record.Attrs(func(attribute slog.Attr) bool {
		seen[attribute.Key] = struct{}{}
		clean.AddAttrs(redact(attribute))
		return true
	})
	if requestID, ok := model.RequestIDFromContext(ctx); ok {
		if _, exists := seen["request_id"]; !exists {
			clean.AddAttrs(slog.String("request_id", requestID.String()))
		}
	}
	if trace, ok := tracecontext.FromContext(ctx); ok {
		if _, exists := seen["trace_id"]; !exists && trace.TraceID != "" {
			clean.AddAttrs(slog.String("trace_id", trace.TraceID))
		}
		if _, exists := seen["span_id"]; !exists && trace.SpanID != "" {
			clean.AddAttrs(slog.String("span_id", trace.SpanID))
		}
	}
	return handler.next.Handle(ctx, clean)
}

func (handler redactingHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, 0, len(attributes))
	for _, attribute := range attributes {
		clean = append(clean, redact(attribute))
	}
	return redactingHandler{next: handler.next.WithAttrs(clean)}
}

func (handler redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{next: handler.next.WithGroup(name)}
}

func redact(attribute slog.Attr) slog.Attr {
	key := strings.ToLower(attribute.Key)
	if isSensitiveKey(key) {
		return slog.String(attribute.Key, "[REDACTED]")
	}
	if attribute.Value.Kind() == slog.KindGroup {
		items := attribute.Value.Group()
		clean := make([]any, 0, len(items))
		for _, item := range items {
			clean = append(clean, redact(item))
		}
		return slog.Group(attribute.Key, clean...)
	}
	return attribute
}

func isSensitiveKey(key string) bool {
	for _, candidate := range []string{
		"token", "secret", "password", "cookie", "authorization", "jwt", "webhook",
		"open_id", "chat_id", "refresh_token", "app_secret", "api_key", "private_key",
	} {
		if key == candidate || strings.HasSuffix(key, "_"+candidate) {
			return true
		}
	}
	return false
}
