//go:build !windows

package gui

func (a *App) setupTray()                  {}
func (a *App) removeTray()                 {}
func (a *App) UpdateTrayIcon(streaming bool) {}
