package ffmpeg

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vexedaa/vrshare/internal/config"
)

func isGPUEncoder(encoder string) bool {
	return encoder == "nvenc" || encoder == "qsv" || encoder == "amf"
}

// pcmPacketSeconds is how much audio FFmpeg's raw PCM demuxer reads from the
// pipe per packet (4096 samples at 48kHz). A wall-clock stamped packet is
// stamped when its last byte arrives, so its first sample was captured this
// much earlier.
const pcmPacketSeconds = 4096.0 / 48000.0

// tsBaseMargin keeps a session's first output timestamps above zero even with
// the largest audio correction (±5s) applied.
const tsBaseMargin = 10 * time.Second

// BuildArgs returns FFmpeg's arguments. tsBase is the stream session's start:
// every FFmpeg launched during the session must get the same one, so output
// timestamps keep counting up across capture restarts. A zero tsBase leaves
// timestamps absolute.
func BuildArgs(cfg config.Config, resolvedEncoder string, segmentDir string, useDDAgrab bool, tsBase time.Time) []string {
	args := []string{}

	// Reduce stats reporting period from FFmpeg's 500ms default to 50ms.
	args = append(args, "-stats_period", "0.05")

	// Both inputs are stamped with the wall-clock time each packet arrives, and
	// -copyts keeps those absolute so audio and video share one clock. Audio
	// used to be timed by its sample count instead: any audio lost on the way
	// in (a capture hiccup, or chunks dropped while FFmpeg stalled) left it
	// permanently behind the wall-clock video, so sync drifted and FFmpeg's
	// interleaver throttled video capture to wait for audio that never caught
	// up. Now a gap in arriving audio is just a gap, which aresample fills.
	args = append(args, "-copyts")

	useDD := useDDAgrab && runtime.GOOS == "windows"

	if useDD {
		// ddagrab is a lavfi source filter, not an input device.
		// Framerate and monitor index are set as filter options.
		lavfiSrc := fmt.Sprintf("ddagrab=output_idx=%d:framerate=%d", cfg.Monitor, cfg.FPS)
		args = append(args, "-use_wallclock_as_timestamps", "1", "-f", "lavfi", "-i", lavfiSrc)
	} else {
		args = append(args, "-use_wallclock_as_timestamps", "1")
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

	// Audio input (raw PCM from WASAPI capturer via stdin). It is moved back by
	// the demuxer's packet-stamping delay, plus the user's sync correction:
	// a positive AudioOffsetMs means audio lags video, so audio moves earlier.
	if cfg.Audio {
		shift := -(pcmPacketSeconds + float64(cfg.AudioOffsetMs)/1000.0)
		args = append(args, "-itsoffset", fmt.Sprintf("%.4f", shift), "-use_wallclock_as_timestamps", "1",
			"-f", "s16le", "-ar", "48000", "-ac", "2", "-i", "pipe:0")
	}

	switch resolvedEncoder {
	case "nvenc":
		args = append(args, "-c:v", "h264_nvenc", "-preset", "p4", "-tune", "ll")
	case "qsv":
		args = append(args, "-c:v", "h264_qsv", "-preset", "veryfast")
	case "amf":
		args = append(args, "-c:v", "h264_amf", "-quality", "speed")
	default:
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency")
	}

	args = append(args, "-b:v", fmt.Sprintf("%dk", cfg.Bitrate))

	// Video filter chain: ddagrab outputs D3D11 hardware frames.
	// GPU encoders (nvenc/amf/qsv) accept D3D11 frames directly — no
	// hwdownload needed. CPU encoders require hwdownload to get software frames.
	if useDD && isGPUEncoder(resolvedEncoder) {
		// GPU encoder: pass D3D11 hw frames directly to encoder.
		// If scaling is requested, we must download to CPU for that.
		if cfg.Resolution != "" {
			scaled := strings.Replace(cfg.Resolution, "x", ":", 1)
			args = append(args, "-vf", "hwdownload,format=bgra,scale="+scaled+",format=yuv420p")
		}
	} else if useDD {
		// CPU encoder: must download hw frames to software
		vf := "hwdownload,format=bgra,format=yuv420p"
		if cfg.Resolution != "" {
			scaled := strings.Replace(cfg.Resolution, "x", ":", 1)
			vf += ",scale=" + scaled
		}
		args = append(args, "-vf", vf)
	} else if !useDD {
		// gdigrab outputs BGRA which needs conversion to yuv420p for encoding.
		vf := "format=yuv420p"
		if cfg.Resolution != "" {
			scaled := strings.Replace(cfg.Resolution, "x", ":", 1)
			vf = "scale=" + scaled + "," + vf
		}
		args = append(args, "-vf", vf)
	}

	// Audio encoding. aresample keeps the samples on their wall-clock
	// timestamps: a gap or burst over 100ms (lost capture, or a backlog read all
	// at once after a stall) is filled with silence or trimmed at once, and a
	// smaller lasting error over 20ms is absorbed by stretching at most 0.5%,
	// which is inaudible. Below 20ms it leaves arrival jitter alone. An optional
	// volume filter is chained ahead of it.
	if cfg.Audio {
		af := "aresample=async=1:min_comp=0.02:min_hard_comp=0.1:max_soft_comp=0.005"
		if cfg.AudioGain != 0 {
			af = fmt.Sprintf("volume=%ddB,%s", cfg.AudioGain, af)
		}
		args = append(args, "-af", af, "-c:a", "aac", "-b:a", "128k")
	}

	gop := fmt.Sprintf("%d", cfg.FPS)
	args = append(args, "-g", gop, "-keyint_min", gop)

	// Rebase the absolute timestamps to just before the session started, so
	// they start small rather than wrapping the 33-bit MPEG-TS clock at some
	// arbitrary moment, and keep counting up across capture restarts.
	if !tsBase.IsZero() {
		base := tsBase.Add(-tsBaseMargin)
		args = append(args, "-output_ts_offset", fmt.Sprintf("%.6f", -float64(base.UnixMicro())/1e6))
	}

	// hls_list_size of 6 gives viewers ~6 seconds to fetch a segment before it
	// rolls off the playlist. hls_delete_threshold keeps 3 extra rolled-off
	// segments on disk as a buffer for slow clients (tunnel latency, VRChat's
	// slow polling cadence). Together: ~9 seconds of segment availability.
	args = append(args,
		"-f", "hls",
		"-hls_time", "1",
		"-hls_list_size", "6",
		"-hls_flags", "append_list+delete_segments",
		"-hls_delete_threshold", "3",
		"-hls_segment_filename", filepath.Join(segmentDir, "segment_%d.ts"),
		filepath.Join(segmentDir, "stream.m3u8"),
	)

	return args
}
