package ffmpeg

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vexedaa/vrshare/internal/config"
)

func BuildArgs(cfg config.Config, resolvedEncoder string, segmentDir string) []string {
	args := []string{}

	// Input: platform-specific screen capture
	switch runtime.GOOS {
	case "linux":
		args = append(args, "-f", "x11grab")
	case "darwin":
		args = append(args, "-f", "avfoundation")
	default: // windows
		args = append(args, "-f", "gdigrab")
	}

	args = append(args, "-framerate", fmt.Sprintf("%d", cfg.FPS))

	// Input source
	switch runtime.GOOS {
	case "linux":
		args = append(args, "-i", fmt.Sprintf(":%d.0", cfg.Monitor))
	case "darwin":
		args = append(args, "-i", fmt.Sprintf("%d", cfg.Monitor))
	default: // windows
		if cfg.Monitor == 0 {
			args = append(args, "-i", "desktop")
		} else {
			// gdigrab uses offset-based approach; for multi-monitor,
			// we specify the display device title
			args = append(args, "-i", "desktop")
			args = append(args, "-offset_x", "0", "-offset_y", "0")
		}
	}

	// Encoder
	switch resolvedEncoder {
	case "nvenc":
		args = append(args, "-c:v", "h264_nvenc", "-preset", "p4", "-tune", "ll")
	case "qsv":
		args = append(args, "-c:v", "h264_qsv", "-preset", "veryfast")
	case "amf":
		args = append(args, "-c:v", "h264_amf", "-quality", "speed")
	default: // cpu
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency")
	}

	// Bitrate
	args = append(args, "-b:v", fmt.Sprintf("%dk", cfg.Bitrate))

	// Resolution scaling
	if cfg.Resolution != "" {
		scaled := strings.Replace(cfg.Resolution, "x", ":", 1)
		args = append(args, "-vf", fmt.Sprintf("scale=%s", scaled))
	}

	// Keyframe interval = 1 per second
	gop := fmt.Sprintf("%d", cfg.FPS)
	args = append(args, "-g", gop, "-keyint_min", gop)

	// HLS output
	args = append(args,
		"-f", "hls",
		"-hls_time", "1",
		"-hls_list_size", "3",
		"-hls_flags", "delete_segments+append_list",
		"-hls_segment_filename", filepath.Join(segmentDir, "segment_%d.ts"),
		filepath.Join(segmentDir, "stream.m3u8"),
	)

	return args
}
