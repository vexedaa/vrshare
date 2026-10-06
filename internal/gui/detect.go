package gui

import (
	"log"
	"sync"
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

		// Test-encode rather than just checking `ffmpeg -encoders`: FFmpeg
		// builds list every vendor's encoder whatever GPU is installed, so a
		// listing check marked NVENC "available" on AMD machines and the wizard
		// picked it (issue #3). Probes run concurrently to stay well inside
		// DetectSystem's timeout.
		probe := ffmpeg.ProbeFFmpegEncoder(ffmpegPath)
		encoders := []struct {
			name, typ, label string
		}{
			{"h264_nvenc", "nvenc", "NVIDIA NVENC"},
			{"h264_qsv", "qsv", "Intel Quick Sync"},
			{"h264_amf", "amf", "AMD AMF"},
			{"libx264", "cpu", "CPU (libx264)"},
		}
		avail := make([]bool, len(encoders))
		var wg sync.WaitGroup
		for i, e := range encoders {
			wg.Add(1)
			go func() {
				defer wg.Done()
				avail[i] = probe(e.name)
			}()
		}
		wg.Wait()
		for i, e := range encoders {
			log.Printf("[detect] Encoder %s: available=%v", e.name, avail[i])
			info.Encoders = append(info.Encoders, server.EncoderInfo{
				Name: e.name, Type: e.typ, Label: e.label, Available: avail[i],
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
		AudioDevices: []server.AudioDevice{server.SystemAudioDevice},
	}
}
