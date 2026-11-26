package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/heiyeluren/mz-gui/examples/framework-agentd-showcase/generated/appmeta"
	"github.com/heiyeluren/mz-gui/framework/go/transport/local"
	webassets "github.com/heiyeluren/mz-gui/framework/web"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed frontend/*
var frontendAssets embed.FS

type backendCloser interface {
	Close(context.Context) error
}

func main() {
	runDir, err := showcaseRunDir()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		log.Fatal(err)
	}
	var backend TaskBackend
	if os.Getenv("MZGUI_TASK_MODE") == "inprocess" {
		backend, err = newLocalTaskBackend(context.Background(), runDir)
	} else {
		connectContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		backend, err = connectAgent(connectContext, local.Endpoint{Name: "showcase-agentd", RunDir: runDir})
		cancel()
	}
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = backend.(backendCloser).Close(context.Background()) }()
	service := &DesktopService{backend: backend}
	mux := http.NewServeMux()
	mux.Handle("/framework/", http.StripPrefix("/framework/", webassets.TokenHandler()))
	mux.Handle("/", application.BundledAssetFileServer(frontendAssets))
	app := application.New(application.Options{
		Name: appmeta.DisplayName, Description: appmeta.Description,
		Assets: application.AssetOptions{Handler: mux}, Services: []application.Service{application.NewService(service)},
		Mac: application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "黑夜路人的 APP · 多进程", Width: 1120, Height: 720,
		MinWidth: 900, MinHeight: 600, URL: "/", BackgroundColour: application.NewRGB(245, 246, 248),
	})
	window.Show()
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func showcaseRunDir() (string, error) {
	runDir := os.Getenv("MZGUI_AGENTD_RUN_DIR")
	if runDir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		runDir = filepath.Join(cache, "mzgui-agentd-showcase", "run")
	}
	if !filepath.IsAbs(runDir) {
		return "", fmt.Errorf("MZGUI_AGENTD_RUN_DIR 必须是绝对路径")
	}
	return runDir, nil
}
