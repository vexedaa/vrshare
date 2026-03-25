//go:build !windows

package gui

import "github.com/vexedaa/vrshare/internal/server"

func detectPlatformDevices() ([]server.MonitorInfo, []server.AudioDevice) {
	return []server.MonitorInfo{
			{Index: 0, Name: "Primary Display", Resolution: "unknown", IsPrimary: true},
		}, []server.AudioDevice{
			{Name: "Default Output Device", IsDefault: true},
		}
}
