package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/panicguard"
	"github.com/heiyeluren/mz-gui/framework/go/lifecycle/wailsapp"
	"github.com/heiyeluren/mz-gui/framework/go/storage/blobquota"
	"github.com/heiyeluren/mz-gui/framework/go/storage/configfile"
)

const showcaseShellConfigName = "showcase-shell.json"
const showcaseBlobCacheName = "blob-cache"
const showcaseBlobQuotaBytes int64 = 64 << 20
const showcaseBlobQuotaMaxAge = 7 * 24 * time.Hour

func (service *ShowcaseService) prepareFrameworkShell(ctx context.Context) error {
	if service == nil {
		return fmt.Errorf("showcase 服务无效")
	}
	dir := service.shellDir
	if dir == "" {
		dir = service.windowStateDir
	}
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(cache, "mz-gui-framework-showcase")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	dto := wailsapp.GuardedCall(ctx, panicguard.Options{Boundary: "showcase.shell.config"}, func(context.Context) error {
		return watchShowcaseShellConfig(dir)
	})
	if dto.Code != "" {
		return fmt.Errorf("监视 showcase 配置失败: %s", dto.ReasonCode)
	}

	dto = wailsapp.GuardedCall(ctx, panicguard.Options{Boundary: "showcase.shell.blobquota"}, func(context.Context) error {
		return pruneShowcaseBlobCache(dir)
	})
	if dto.Code != "" {
		return fmt.Errorf("清理 showcase Blob 缓存失败: %s", dto.ReasonCode)
	}

	dto = wailsapp.GuardedProduce(ctx, nil, panicguard.Options{Boundary: "showcase.shell.ready"}, func(context.Context) error {
		_, err := os.Stat(filepath.Join(dir, showcaseShellConfigName))
		return err
	})
	if dto.Code != "" {
		return fmt.Errorf("确认 showcase 配置失败: %s", dto.ReasonCode)
	}

	if err := produceShowcaseShellReady(ctx, dir); err != nil {
		return fmt.Errorf("确认 showcase Produce 边界失败: %w", err)
	}

	path, err := produceShowcaseShellConfigPath(ctx, dir)
	if err != nil {
		return fmt.Errorf("读取 showcase 配置路径失败: %w", err)
	}
	if path == "" {
		return fmt.Errorf("showcase 配置路径为空")
	}
	if err := guardShowcaseShellReady(ctx, dir); err != nil {
		return fmt.Errorf("确认 showcase Guard 边界失败: %w", err)
	}
	if err := guardShowcaseShellRequest(ctx, dir); err != nil {
		return fmt.Errorf("确认 showcase GuardRequest 边界失败: %w", err)
	}
	if err := protectShowcaseShell(ctx); err != nil {
		return fmt.Errorf("确认 showcase 韧性边界失败: %w", err)
	}
	if err := probeShowcaseAdapters(ctx); err != nil {
		return fmt.Errorf("确认 showcase 适配器契约失败: %w", err)
	}
	if err := probeShowcaseStreamBoundary(ctx); err != nil {
		return fmt.Errorf("确认 showcase gRPC stream 边界失败: %w", err)
	}
	if err := bootstrapMinimalNewApp(ctx, dir); err != nil {
		return fmt.Errorf("确认新 APP candidate 启动路径失败: %w", err)
	}
	return nil
}

func watchShowcaseShellConfig(dir string) error {
	store := configfile.New(configfile.Options{
		Path:     filepath.Join(dir, showcaseShellConfigName),
		MaxBytes: 4096,
		Initial:  []byte("{}\n"),
	})
	watcher, err := configfile.NewWatcher(configfile.WatchOptions{
		Store:    store,
		Interval: time.Second,
	})
	if err != nil {
		return err
	}
	return watcher.Poll()
}

func pruneShowcaseBlobCache(dir string) error {
	root := filepath.Join(dir, showcaseBlobCacheName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	_, err := blobquota.Prune(root, showcaseBlobQuotaBytes, showcaseBlobQuotaMaxAge, time.Now().UTC())
	return err
}
