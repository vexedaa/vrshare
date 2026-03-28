package ffmpeg

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vexedaa/vrshare/internal/config"
)

func isGPUEncoder(encoder string) bool {
	return encoder == "nvenc" || encoder == "qsv" || encoder == "amf"
}

func BuildArgs(cfg config.Config, resolvedEncoder string, segmentDir string, useDDAgrab bool) []string {
	args := []string{}

	useDD := useDDAgrab && runtime.GOOS == "windows"

	if useDD {
		// ddagrab is a lavfi source filter, not an input device.
		// Framerate and monitor index are set as filter options.
		lavfiSrc := fmt.Sprintf("ddagrab=output_idx=%d:framerate=%d", cfg.Monitor, cfg.FPS)
		args = append(args, "-f", "lavfi", "-i", lavfiSrc)
	} else {
		switch runtime.GOOS {
		case "linux":
			args = append(args, "-f", "x11grab")
		case "darwin":
			args = append(args, "-f", "avfoundation")
		default:
			args = append(args, "-f", "gdigrab")
		}

		args = append(args, "-framerate", fmt.Sprintf("%d", cfg.FPS))

		switch runtime.GOOS {
		case "linux":
			args = append(args, "-i", fmt.Sprintf(":%d.0", cfg.Monitor))
		case "darwin":
			args = append(args, "-i", fmt.Sprintf("%d", cfg.Monitor))
		default:
			if cfg.Monitor == 0 {
				args = append(args, "-i", "desktop")
			} else {
				args = append(args, "-i", "desktop")
				args = append(args, "-offset_x", "0", "-offset_y", "0")
			}
		}
	}

	// Audio input (raw PCM from WASAPI capturer via stdin)
	if cfg.Audio {
		args = append(args, "-f", "s16le", "-ar", "48000", "-ac", "2", "-i", "pipe:0")
	}

	switch resolvedEncoder {
	case "nvenc":
		args = append(args, "-c:v", "h264_nvenc", "-preset", "p4", "-tune", "ll",
			"-profile:v", "baseline", "-level:v", "auto")
	case "qsv":
		args = append(args, "-c:v", "h264_qsv", "-preset", "veryfast",
			"-profile:v", "baseline", "-level:v", "auto")
	case "amf":
		args = append(args, "-c:v", "h264_amf", "-quality", "speed",
			"-profile:v", "baseline", "-level:v", "auto")
	default:
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
			"-profile:v", "baseline", "-level:v", "auto")
	}

	args = append(args, "-b:v", fmt.Sprintf("%dk", cfg.Bitrate))

	// Video filter chain: ddagrab outputs D3D11 hardware frames which need
	// hwdownload for any software processing (scaling, format conversion).
	if useDD {
		vf := "hwdownload,format=bgra,format=yuv420p"
		if cfg.Resolution != "" {
			scaled := strings.Replace(cfg.Resolution, "x", ":", 1)
			vf += ",scale=" + scaled
		}
		args = append(args, "-vf", vf)
	} else if cfg.Resolution != "" {
		scaled := strings.Replace(cfg.Resolution, "x", ":", 1)
		args = append(args, "-vf", fmt.Sprintf("scale=%s", scaled))
	}

	// Audio encoding
	if cfg.Audio {
		if cfg.AudioGain != 0 {
			args = append(args, "-af", fmt.Sprintf("volume=%ddB", cfg.AudioGain))
		}
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	}

	gop := fmt.Sprintf("%d", cfg.FPS)
	args = append(args, "-g", gop, "-keyint_min", gop)

	args = append(args,
		"-f", "hls",
		"-hls_time", "1",
		"-hls_list_size", "2",
		"-hls_flags", "append_list+delete_segments",
		"-hls_segment_filename", filepath.Join(segmentDir, "segment_%d.ts"),
		filepath.Join(segmentDir, "stream.m3u8"),
	)

	return args
}
