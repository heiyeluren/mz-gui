package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/panicguard"
)

func produceShowcaseShellReady(ctx context.Context, dir string) error {
	if ctx == nil {
		return fmt.Errorf("showcase Produce 上下文无效")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("showcase 目录无效")
	}
	return panicguard.Produce(ctx, nil, panicguard.Options{Boundary: "showcase.shell.produce"}, func(context.Context) error {
		_, err := os.Stat(filepath.Join(dir, showcaseShellConfigName))
		return err
	})
}

func produceShowcaseShellConfigPath(ctx context.Context, dir string) (string, error) {
	return panicguard.ProduceResult(ctx, nil, panicguard.Options{Boundary: "showcase.shell.config-path"}, func(context.Context) (string, error) {
		path := filepath.Join(dir, showcaseShellConfigName)
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("showcase 配置路径不是文件")
		}
		return path, nil
	})
}

func guardShowcaseShellReady(ctx context.Context, dir string) error {
	if ctx == nil {
		return fmt.Errorf("showcase Guard 上下文无效")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("showcase 目录无效")
	}
	service, err := panicguard.NewService("showcase-shell")
	if err != nil {
		return err
	}
	return service.Guard(ctx, "ready", func(context.Context) error {
		_, err := os.Stat(filepath.Join(dir, showcaseShellConfigName))
		return err
	})
}

func guardShowcaseShellRequest(ctx context.Context, dir string) error {
	if ctx == nil {
		return fmt.Errorf("showcase GuardRequest 上下文无效")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("showcase 目录无效")
	}
	service, err := panicguard.NewService("showcase-shell")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	meta, err := showcaseRequestMeta(now)
	if err != nil {
		return err
	}
	return service.GuardRequest(ctx, now, meta, "ready", func(context.Context) error {
		_, err := os.Stat(filepath.Join(dir, showcaseShellConfigName))
		return err
	})
}
