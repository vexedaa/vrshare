package gui

import (
	"github.com/vexedaa/vrshare/internal/ffmpeg"
	"github.com/vexedaa/vrshare/internal/server"
)

// DetectSystem probes the system for available encoders, monitors, and audio devices.
func (a *App) DetectSystem() server.SystemInfo {
	info := server.SystemInfo{}

	ffmpegPath, err := ffmpeg.FindFFmpeg()
	if err == nil {
		probe := ffmpeg.ProbeFFmpegEncoder(ffmpegPath)
		encoders := []struct {
			name  string
			typ   string
			label string
			enc   string
		}{
			{"h264_nvenc", "nvenc", "NVIDIA NVENC", "h264_nvenc"},
			{"h264_qsv", "qsv", "Intel Quick Sync", "h264_qsv"},
			{"h264_amf", "amf", "AMD AMF", "h264_amf"},
			{"libx264", "cpu", "CPU (libx264)", "libx264"},
		}
		for _, e := range encoders {
			info.Encoders = append(info.Encoders, server.EncoderInfo{
				Name:      e.name,
				Type:      e.typ,
				Label:     e.label,
				Available: probe(e.enc),
			})
		}
	}

	info.Monitors, info.AudioDevices = detectPlatformDevices()

	return info
}
