package main

import (
	"context"
	"embed"
	"log"
	"net/http"

	"github.com/heiyeluren/mz-gui/examples/framework-showcase/generated/appmeta"
	wailslifecycle "github.com/heiyeluren/mz-gui/framework/go/lifecycle/wailsapp"
	webassets "github.com/heiyeluren/mz-gui/framework/web"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed frontend/*
var frontendAssets embed.FS

func main() {
	service, err := NewShowcaseService()
	if err != nil {
		log.Fatal(err)
	}
	if err := service.Start(context.Background()); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/framework/", http.StripPrefix("/framework/", webassets.TokenHandler()))
	mux.Handle("/", application.BundledAssetFileServer(frontendAssets))
	app := application.New(application.Options{
		Name:        appmeta.DisplayName,
		Description: appmeta.Description,
		Assets:      application.AssetOptions{Handler: mux},
		Services:    []application.Service{application.NewService(service)},
		OnShutdown: func() {
			_ = service.Stop(context.Background())
		},
		Mac: application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "黑夜路人的 APP", Width: 1120, Height: 720,
		MinWidth: 900, MinHeight: 600, URL: "/",
		BackgroundColour: application.NewRGB(245, 246, 248),
	})
	window.Show()
	windowLifecycle := wailslifecycle.NewController(wailslifecycle.CloseToTray)
	windowLifecycle.Bind(app, window, wailslifecycle.BindOptions{
		ShowWindowLabel: "显示黑夜路人的 APP",
		QuitLabel:       "退出黑夜路人的 APP",
		Tooltip:         "黑夜路人的 APP · MZ-GUI",
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
