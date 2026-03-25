package server

import (
	"time"

	"github.com/vexedaa/vrshare/internal/config"
)

// StreamState represents the current state of the streaming server.
type StreamState struct {
	Status        string        `json:"status"` // "idle", "starting", "streaming", "error"
	Error         string        `json:"error"`
	Uptime        time.Duration `json:"uptime"`
	StreamURL     string        `json:"streamURL"`
	FPS           float64       `json:"fps"`
	Bitrate       int           `json:"bitrate"`
	DroppedFrames int           `json:"droppedFrames"`
	Speed         float64       `json:"speed"`
	ViewerCount   int           `json:"viewerCount"`
}

// AppSettings holds application-level preferences.
type AppSettings struct {
	FirstRunComplete bool   `json:"firstRunComplete"`
	CloseBehavior    string `json:"closeBehavior"` // "tray" or "quit"
}

// DefaultSettings returns default app settings.
func DefaultSettings() AppSettings {
	return AppSettings{
		FirstRunComplete: false,
		CloseBehavior:    "tray",
	}
}

// Preset is a named configuration snapshot.
type Preset struct {
	Name   string        `json:"name"`
	Config config.Config `json:"config"`
}

// DefaultPreset returns the default preset created on first run.
func DefaultPreset() Preset {
	return Preset{
		Name: "Default",
		Config: config.Config{
			Port:       8080,
			FPS:        60,
			Bitrate:    4000,
			Encoder:    config.EncoderAuto,
			Audio:      true,
			Resolution: "1920x1080",
		},
	}
}

// SystemInfo holds detected system capabilities.
type SystemInfo struct {
	Encoders     []EncoderInfo `json:"encoders"`
	Monitors     []MonitorInfo `json:"monitors"`
	AudioDevices []AudioDevice `json:"audioDevices"`
}

// EncoderInfo describes an available encoder.
type EncoderInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
}

// MonitorInfo describes a display monitor.
type MonitorInfo struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	Resolution string `json:"resolution"`
	IsPrimary  bool   `json:"isPrimary"`
}

// AudioDevice describes an audio output device.
type AudioDevice struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}
