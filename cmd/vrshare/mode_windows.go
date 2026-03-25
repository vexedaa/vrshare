//go:build windows

package main

import (
	"log"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/vexedaa/vrshare/frontend"
	"github.com/vexedaa/vrshare/internal/gui"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
)

func hasConsole() bool {
	hwnd, _, _ := procGetConsoleWindow.Call()
	return hwnd != 0
}

func launchGUI() {
	app := gui.NewApp()

	err := wails.Run(&options.App{
		Title:     "VRShare",
		Width:     900,
		Height:    600,
		MinWidth:  700,
		MinHeight: 400,
		AssetServer: &assetserver.Options{
			Assets: frontend.Assets,
		},
		OnStartup:    app.Startup,
		OnShutdown:   app.Shutdown,
		OnBeforeClose: app.BeforeClose,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
