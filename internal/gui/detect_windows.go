//go:build windows

package gui

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/vexedaa/vrshare/internal/server"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

func detectPlatformDevices() ([]server.MonitorInfo, []server.AudioDevice) {
	monitors := detectMonitors()
	audioDevices := []server.AudioDevice{
		{Name: "Default Output Device", IsDefault: true},
	}
	return monitors, audioDevices
}

func detectMonitors() []server.MonitorInfo {
	// Use GetSystemMetrics instead of EnumDisplayMonitors to avoid
	// callback-based API that deadlocks when called from a Wails goroutine.
	const SM_CMONITORS = 80
	const SM_CXSCREEN = 0
	const SM_CYSCREEN = 1

	count, _, _ := procGetSystemMetrics.Call(SM_CMONITORS)
	if count == 0 {
		count = 1
	}

	w, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	h, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)

	var monitors []server.MonitorInfo
	for i := 0; i < int(count); i++ {
		res := fmt.Sprintf("%dx%d", w, h)
		if i > 0 {
			res = "unknown" // GetSystemMetrics only returns primary dimensions
		}
		monitors = append(monitors, server.MonitorInfo{
			Index:      i,
			Name:       fmt.Sprintf("Monitor %d", i),
			Resolution: res,
			IsPrimary:  i == 0,
		})
	}

	return monitors
}

// Keep unsafe import used
var _ = unsafe.Pointer(nil)
