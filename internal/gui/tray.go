package gui

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ShowWindow brings the main window to front.
func (a *App) ShowWindow() {
	runtime.WindowShow(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
}
