//go:build windows

package gui

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/vexedaa/vrshare/internal/server"
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitorsW")
	procGetMonitorInfo      = user32.NewProc("GetMonitorInfoW")
)

type monitorInfoEx struct {
	cbSize    uint32
	rcMonitor struct{ left, top, right, bottom int32 }
	rcWork    struct{ left, top, right, bottom int32 }
	dwFlags   uint32
	szDevice  [32]uint16
}

func detectPlatformDevices() ([]server.MonitorInfo, []server.AudioDevice) {
	monitors := detectMonitors()
	audioDevices := []server.AudioDevice{
		{Name: "Default Output Device", IsDefault: true},
	}
	return monitors, audioDevices
}

func detectMonitors() []server.MonitorInfo {
	var monitors []server.MonitorInfo
	idx := 0

	callback := syscall.NewCallback(func(hMonitor uintptr, hdc uintptr, lprcClip uintptr, dwData uintptr) uintptr {
		var mi monitorInfoEx
		mi.cbSize = uint32(unsafe.Sizeof(mi))
		procGetMonitorInfo.Call(hMonitor, uintptr(unsafe.Pointer(&mi)))

		w := mi.rcMonitor.right - mi.rcMonitor.left
		h := mi.rcMonitor.bottom - mi.rcMonitor.top
		isPrimary := mi.dwFlags&1 != 0

		monitors = append(monitors, server.MonitorInfo{
			Index:      idx,
			Name:       fmt.Sprintf("Monitor %d", idx),
			Resolution: fmt.Sprintf("%dx%d", w, h),
			IsPrimary:  isPrimary,
		})
		idx++
		return 1
	})

	procEnumDisplayMonitors.Call(0, 0, callback, 0)
	return monitors
}
