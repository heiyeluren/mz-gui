package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/observability/logging"
)

// bootstrapMinimalNewApp 是新 APP 只依赖 Framework candidate 合同的最小生产启动路径。
// 顺序：请求身份 → Produce/Guard → 韧性 → 崩溃诊断 → 适配器契约 → gRPC stream 边界。
func bootstrapMinimalNewApp(ctx context.Context, dir string) error {
	if ctx == nil {
		return fmt.Errorf("新 APP 启动上下文无效")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("新 APP 状态目录无效")
	}
	bound, cancel, err := bindShowcaseRequest(ctx, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("绑定新 APP 请求身份失败: %w", err)
	}
	defer cancel()
	if err := produceShowcaseShellReady(bound, dir); err != nil {
		return fmt.Errorf("新 APP Produce 失败: %w", err)
	}
	if _, err := produceShowcaseShellConfigPath(bound, dir); err != nil {
		return fmt.Errorf("新 APP ProduceResult 失败: %w", err)
	}
	if err := guardShowcaseShellReady(bound, dir); err != nil {
		return fmt.Errorf("新 APP Guard 失败: %w", err)
	}
	if err := guardShowcaseShellRequest(bound, dir); err != nil {
		return fmt.Errorf("新 APP GuardRequest 失败: %w", err)
	}
	if err := protectShowcaseShell(bound); err != nil {
		return fmt.Errorf("新 APP 韧性边界失败: %w", err)
	}
	if err := probeShowcaseCrashDiagnostics(); err != nil {
		return fmt.Errorf("新 APP 崩溃诊断失败: %w", err)
	}
	if err := probeShowcaseAdapters(bound); err != nil {
		return fmt.Errorf("新 APP 适配器契约失败: %w", err)
	}
	if err := probeShowcaseStreamBoundary(bound); err != nil {
		return fmt.Errorf("新 APP gRPC stream 边界失败: %w", err)
	}
	if err := probeShowcaseBusPatterns(bound); err != nil {
		return fmt.Errorf("新 APP Command/Query Bus 失败: %w", err)
	}
	if err := writeShowcaseRuntimeLog(dir); err != nil {
		return fmt.Errorf("新 APP 本机日志失败: %w", err)
	}
	return nil
}

func writeShowcaseRuntimeLog(dir string) error {
	document, _ := os.ReadFile(filepath.Join(dir, "app.toml"))
	settings, err := logging.ParseSettings(document)
	if err != nil {
		return err
	}
	options, err := logging.ApplySettings(logging.Options{
		ProcessName: "showcase-desktop",
		Component:   "desktop",
		Level:       "info",
		MaxBytes:    1 << 20,
	}, settings, filepath.Join(dir, "logs"))
	if err != nil {
		return err
	}
	logger, closer, err := logging.New(options)
	if err != nil {
		return err
	}
	defer closer.Close()
	logger.Info("showcase started")
	return nil
}
