package gui

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/vexedaa/vrshare/internal/ffmpeg"
	"github.com/vexedaa/vrshare/internal/server"
)

// DetectSystem probes the system for available encoders, monitors, and audio devices.
func (a *App) DetectSystem() server.SystemInfo {
	log.Println("[detect] DetectSystem called")

	ch := make(chan server.SystemInfo, 1)
	go func() {
		ch <- detectSystemImpl()
	}()

	select {
	case info := <-ch:
		log.Println("[detect] DetectSystem completed normally")
		return info
	case <-time.After(8 * time.Second):
		log.Println("[detect] DetectSystem timed out, using fallback")
		return fallbackSystemInfo()
	}
}

func detectSystemImpl() server.SystemInfo {
	info := server.SystemInfo{}

	log.Println("[detect] Finding FFmpeg...")
	ffmpegPath, err := ffmpeg.FindFFmpeg()
	if err != nil {
		log.Printf("[detect] FFmpeg not found: %v", err)
		info.Encoders = []server.EncoderInfo{
			{Name: "h264_nvenc", Type: "nvenc", Label: "NVIDIA NVENC", Available: false},
			{Name: "h264_qsv", Type: "qsv", Label: "Intel Quick Sync", Available: false},
			{Name: "h264_amf", Type: "amf", Label: "AMD AMF", Available: false},
			{Name: "libx264", Type: "cpu", Label: "CPU (libx264)", Available: false},
		}
	} else {
		log.Printf("[detect] FFmpeg found at: %s", ffmpegPath)
		log.Println("[detect] Running encoder probe...")
		encoderList := runHidden(ffmpegPath, "-hide_banner", "-encoders")
		log.Printf("[detect] Encoder probe returned %d bytes", len(encoderList))

		encoders := []struct {
			name, typ, label, enc string
		}{
			{"h264_nvenc", "nvenc", "NVIDIA NVENC", "h264_nvenc"},
			{"h264_qsv", "qsv", "Intel Quick Sync", "h264_qsv"},
			{"h264_amf", "amf", "AMD AMF", "h264_amf"},
			{"libx264", "cpu", "CPU (libx264)", "libx264"},
		}
		for _, e := range encoders {
			avail := strings.Contains(encoderList, e.enc)
			log.Printf("[detect] Encoder %s: available=%v", e.name, avail)
			info.Encoders = append(info.Encoders, server.EncoderInfo{
				Name: e.name, Type: e.typ, Label: e.label, Available: avail,
			})
		}
	}

	log.Println("[detect] Detecting platform devices...")
	info.Monitors, info.AudioDevices = detectPlatformDevices()
	log.Printf("[detect] Found %d monitors, %d audio devices", len(info.Monitors), len(info.AudioDevices))

	return info
}

func fallbackSystemInfo() server.SystemInfo {
	return server.SystemInfo{
		Encoders: []server.EncoderInfo{
			{Name: "auto", Type: "auto", Label: "Auto (detect on start)", Available: true},
		},
		Monitors: []server.MonitorInfo{
			{Index: 0, Name: "Primary Display", Resolution: "auto", IsPrimary: true},
		},
		AudioDevices: []server.AudioDevice{
			{Name: "Default Output Device", IsDefault: true},
		},
	}
}

func runHidden(name string, args ...string) string {
	log.Printf("[detect] runHidden: %s %v", name, args)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	out, err := cmd.Output()
	if err != nil {
		log.Printf("[detect] runHidden error: %v", err)
		return ""
	}
	log.Printf("[detect] runHidden success: %d bytes", len(out))
	return string(out)
}
