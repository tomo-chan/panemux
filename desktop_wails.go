//go:build darwin || linux

package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"sync"
	"time"

	"panemux/internal/app"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed desktop_assets
var desktopAssets embed.FS

func runDesktop(opts cliOptions) error {
	desktopAssetFS, err := fs.Sub(desktopAssets, "desktop_assets")
	if err != nil {
		return fmt.Errorf("prepare desktop assets: %w", err)
	}

	runtimeApp, err := app.Bootstrap(app.Options{
		ConfigPath: opts.configPath,
		Mode:       app.ModeDesktop,
	}, frontendFS)
	if err != nil {
		return fmt.Errorf("bootstrap desktop mode: %w", err)
	}

	runtimeApp.Start()
	log.Printf("Listening on %s", runtimeApp.BaseURL)

	var shutdownOnce sync.Once
	var redirectOnce sync.Once
	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := runtimeApp.Shutdown(ctx); shutdownErr != nil {
			log.Printf("desktop shutdown error: %v", shutdownErr)
		}
	}
	defer shutdown()

	baseURLJSON, err := json.Marshal(runtimeApp.BaseURL)
	if err != nil {
		return fmt.Errorf("marshal desktop base URL: %w", err)
	}

	if err := wails.Run(&options.App{
		Title:     "panemux",
		Width:     1440,
		Height:    960,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: desktopAssetFS,
		},
		OnStartup: func(ctx context.Context) {
			go func() {
				if err := <-runtimeApp.Errors(); err != nil {
					log.Printf("desktop backend error: %v", err)
				}
				wailsruntime.Quit(ctx)
			}()
		},
		OnDomReady: func(ctx context.Context) {
			redirectOnce.Do(func() {
				wailsruntime.WindowExecJS(ctx, fmt.Sprintf("window.location.replace(%s);", string(baseURLJSON)))
			})
		},
		OnShutdown: func(context.Context) {
			shutdownOnce.Do(shutdown)
		},
	}); err != nil {
		return fmt.Errorf("run desktop shell: %w", err)
	}

	return nil
}
