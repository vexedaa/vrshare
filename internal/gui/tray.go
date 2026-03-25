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

// updateWindowTitle sets the window title based on stream state.
func (a *App) updateWindowTitle() {
	state := a.srv.State()
	if state.Status == "streaming" {
		runtime.WindowSetTitle(a.ctx, "VRShare - Streaming")
	} else {
		runtime.WindowSetTitle(a.ctx, "VRShare")
	}
}
