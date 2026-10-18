package ffmpeg

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/vexedaa/vrshare/internal/config"
)

func TestBuildArgs_Defaults(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false, time.Time{})

	// Stats period must be present so OnFirstFrame fires quickly (~50ms)
	assertContains(t, args, "-stats_period", "0.05")
	assertContains(t, args, "-f", "gdigrab")
	assertContains(t, args, "-framerate", "30")
	assertContains(t, args, "-i", "desktop")
	assertContains(t, args, "-c:v", "libx264")
	assertContains(t, args, "-b:v", "4000k")
	assertContains(t, args, "-g", "30")
	assertContains(t, args, "-f", "hls")
	assertContains(t, args, "-hls_time", "1")
	assertContains(t, args, "-hls_list_size", "6")
	assertContains(t, args, "-hls_flags", "append_list+delete_segments")
	assertContains(t, args, "-hls_delete_threshold", "3")
	assertContains(t, args, "-vf", "format=yuv420p")
}

// TestBuildArgs_StatsPeriod verifies that -stats_period 0.05 is present in
// every BuildArgs call so that StatsParser.OnFirstFrame fires within ~50ms of
// the first encoded frame, keeping A/V startup offset imperceptible.
func TestBuildArgs_StatsPeriod(t *testing.T) {
	cases := []struct {
		name       string
		encoder    string
		useDDAgrab bool
	}{
		{"cpu+gdigrab", "cpu", false},
		{"nvenc+ddagrab", "nvenc", true},
		{"cpu+ddagrab", "cpu", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			args := BuildArgs(cfg, tc.encoder, "/tmp/vrshare", tc.useDDAgrab, time.Time{})
			assertContains(t, args, "-stats_period", "0.05")
		})
	}
}

func TestBuildArgs_CustomFPSAndBitrate(t *testing.T) {
	cfg := config.Default()
	cfg.FPS = 60
	cfg.Bitrate = 6000
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false, time.Time{})

	assertContains(t, args, "-framerate", "60")
	assertContains(t, args, "-b:v", "6000k")
	assertContains(t, args, "-g", "60")
}

func TestBuildArgs_WithResolution(t *testing.T) {
	cfg := config.Default()
	cfg.Resolution = "1280x720"
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false, time.Time{})

	assertContains(t, args, "-vf", "scale=1280:720,format=yuv420p")
}

func TestBuildArgs_NVENCEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", false, time.Time{})

	assertContains(t, args, "-c:v", "h264_nvenc")
	assertContains(t, args, "-preset", "p4")
	assertContains(t, args, "-tune", "ll")
}

func TestBuildArgs_QSVEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "qsv", "/tmp/vrshare", false, time.Time{})

	assertContains(t, args, "-c:v", "h264_qsv")
	assertContains(t, args, "-preset", "veryfast")
}

func TestBuildArgs_AMFEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "amf", "/tmp/vrshare", false, time.Time{})

	assertContains(t, args, "-c:v", "h264_amf")
	assertContains(t, args, "-quality", "speed")
}

func TestBuildArgs_CPUEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false, time.Time{})

	assertContains(t, args, "-c:v", "libx264")
	assertContains(t, args, "-preset", "veryfast")
	assertContains(t, args, "-tune", "zerolatency")
}

func TestBuildArgs_OutputPaths(t *testing.T) {
	cfg := config.Default()
	dir := t.TempDir()
	args := BuildArgs(cfg, "cpu", dir, false, time.Time{})

	expectedSeg := filepath.Join(dir, "segment_%d.ts")
	assertContains(t, args, "-hls_segment_filename", expectedSeg)
	// last arg should be the playlist path
	expectedPlaylist := filepath.Join(dir, "stream.m3u8")
	if args[len(args)-1] != expectedPlaylist {
		t.Errorf("last arg should be playlist path, got %q", args[len(args)-1])
	}
}

func TestBuildArgs_DDAgrab_GPUEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true, time.Time{})
	assertContains(t, args, "-f", "lavfi")
	assertContains(t, args, "-i", "ddagrab=output_idx=0:framerate=30")
	assertContains(t, args, "-c:v", "h264_nvenc")
	// GPU encoder: no hwdownload — D3D11 hw frames passed directly
	assertNotContains(t, args, "-vf")
}

func TestBuildArgs_DDAgrab_GPUEncoder_AMF(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "amf", "/tmp/vrshare", true, time.Time{})
	assertContains(t, args, "-c:v", "h264_amf")
	// AMF should also get D3D11 hw frames directly
	assertNotContains(t, args, "-vf")
}

func TestBuildArgs_DDAgrab_CPUEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", true, time.Time{})
	assertContains(t, args, "-f", "lavfi")
	assertContains(t, args, "-i", "ddagrab=output_idx=0:framerate=30")
	assertContains(t, args, "-c:v", "libx264")
	// CPU encoder must hwdownload
	assertContains(t, args, "-vf", "hwdownload,format=bgra,format=yuv420p")
}

func TestBuildArgs_DDAgrab_CPUEncoder_WithResolution(t *testing.T) {
	cfg := config.Default()
	cfg.Resolution = "1280x720"
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", true, time.Time{})
	assertContains(t, args, "-vf", "hwdownload,format=bgra,format=yuv420p,scale=1280:720")
}

func TestBuildArgs_DDAgrab_GPUEncoder_WithResolution(t *testing.T) {
	cfg := config.Default()
	cfg.Resolution = "1280x720"
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true, time.Time{})
	// Scaling requires hwdownload even for GPU encoders
	assertContains(t, args, "-vf", "hwdownload,format=bgra,scale=1280:720,format=yuv420p")
}

func TestBuildArgs_DDAgrab_MonitorIndex(t *testing.T) {
	cfg := config.Default()
	cfg.Monitor = 2
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true, time.Time{})
	assertContains(t, args, "-i", "ddagrab=output_idx=2:framerate=30")
}

func TestBuildArgs_DDAgrab_CustomFPS(t *testing.T) {
	cfg := config.Default()
	cfg.FPS = 60
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true, time.Time{})
	assertContains(t, args, "-i", "ddagrab=output_idx=0:framerate=60")
	assertContains(t, args, "-g", "60")
	assertNotContains(t, args, "-framerate")
}

func TestBuildArgs_AudioEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Audio = true
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true, time.Time{})

	assertContains(t, args, "-f", "s16le")
	assertContains(t, args, "-ar", "48000")
	assertContains(t, args, "-ac", "2")
	assertContains(t, args, "-i", "pipe:0")
	// Volume is chained ahead of the timestamp corrector.
	assertContains(t, args, "-af", "volume=6dB,aresample=async=1:min_comp=0.02:min_hard_comp=0.1:max_soft_comp=0.005")
	assertContains(t, args, "-c:a", "aac")
	assertContains(t, args, "-b:a", "128k")
	assertNotContains(t, args, "dshow")
}

func TestBuildArgs_AudioGainZero(t *testing.T) {
	cfg := config.Default()
	cfg.Audio = true
	cfg.AudioGain = 0
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false, time.Time{})

	// No volume filter, but the timestamp corrector is always present.
	assertContains(t, args, "-af", "aresample=async=1:min_comp=0.02:min_hard_comp=0.1:max_soft_comp=0.005")
	assertContains(t, args, "-c:a", "aac")
}

// Audio and video must be timestamped by the same clock. Audio used to be
// timed by its sample count and video by the wall clock, so any audio lost on
// the way to FFmpeg (a capture hiccup, or chunks dropped while FFmpeg stalled)
// left audio permanently behind: sync drifted, and FFmpeg's interleaver held
// video capture back to wait for audio that never caught up, collapsing the
// stream to a few frames per second until it was restarted.
func TestBuildArgs_InputsStampedWithWallClock(t *testing.T) {
	cases := []struct {
		name       string
		encoder    string
		useDDAgrab bool
		videoInput string
	}{
		{"ddagrab", "nvenc", true, "ddagrab=output_idx=0:framerate=30"},
		{"gdigrab", "cpu", false, "desktop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := BuildArgs(config.Default(), tc.encoder, "/tmp/vrshare", tc.useDDAgrab, time.Time{})
			assertContains(t, inputOptions(args, tc.videoInput), "-use_wallclock_as_timestamps", "1")
			assertContains(t, inputOptions(args, "pipe:0"), "-use_wallclock_as_timestamps", "1")
			// Without -copyts FFmpeg rebases each input to its own first packet,
			// discarding the shared clock.
			if idxOf(args, "-copyts") < 0 {
				t.Errorf("args should contain -copyts, got %v", args)
			}
		})
	}
}

// FFmpeg's raw PCM demuxer reads the pipe in 4096-sample packets and, with
// wall-clock stamping, stamps each when its last byte arrives: 85.3ms after
// its first sample was captured. The audio input is moved back by that much
// plus the user's correction. Positive AudioOffsetMs means audio lags, so
// audio moves earlier; negative means it leads, so it moves later. The video
// input is never shifted.
func TestBuildArgs_AudioInputOffset(t *testing.T) {
	cases := []struct {
		offsetMs int
		want     string
	}{
		{0, "-0.0853"},
		{200, "-0.2853"},
		{-200, "0.1147"},
	}
	for _, tc := range cases {
		cfg := config.Default()
		cfg.AudioOffsetMs = tc.offsetMs
		args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true, time.Time{})
		assertContains(t, inputOptions(args, "pipe:0"), "-itsoffset", tc.want)
		assertNotContains(t, inputOptions(args, "ddagrab=output_idx=0:framerate=30"), "-itsoffset")
	}
}

// Wall-clock timestamps are rebased to just before the session started, so
// output timestamps start small instead of at the epoch time (which would wrap
// the 33-bit MPEG-TS clock at an arbitrary moment mid-stream).
func TestBuildArgs_OutputTimestampsRebasedToSession(t *testing.T) {
	base := time.Unix(1792280000, 500_000_000)
	args := BuildArgs(config.Default(), "nvenc", "/tmp/vrshare", true, base)

	i := idxOf(args, "-output_ts_offset")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("args should contain -output_ts_offset, got %v", args)
	}
	if i < idxOf(args, "pipe:0") {
		t.Error("-output_ts_offset is an output option; it must follow the inputs")
	}
	off, err := strconv.ParseFloat(args[i+1], 64)
	if err != nil {
		t.Fatalf("-output_ts_offset %q: %v", args[i+1], err)
	}
	// The first timestamps of the session land a little above zero.
	if start := 1792280000.5 + off; start < 0 || start > 60 {
		t.Errorf("session start maps to output time %.3fs, want within [0, 60]", start)
	}

	if noBase := BuildArgs(config.Default(), "nvenc", "/tmp/vrshare", true, time.Time{}); idxOf(noBase, "-output_ts_offset") >= 0 {
		t.Error("a zero base must not rebase output timestamps")
	}
}

func TestBuildArgs_AudioDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Audio = false
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true, time.Time{})

	assertNotContains(t, args, "s16le")
	assertNotContains(t, args, "pipe:0")
	assertNotContains(t, args, "-c:a")
}

func TestBuildArgs_GdigrabFallback(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false, time.Time{})
	assertContains(t, args, "-f", "gdigrab")
	assertContains(t, args, "-i", "desktop")
}

func assertContains(t *testing.T, args []string, key, value string) {
	t.Helper()
	for i, a := range args {
		if a == key && i+1 < len(args) && args[i+1] == value {
			return
		}
	}
	t.Errorf("args should contain %s %s, got %v", key, value, args)
}

// inputOptions returns the options applying to the input "-i value": the args
// between the previous input (or the start) and that -i.
func inputOptions(args []string, value string) []string {
	start := 0
	for i := 0; i+1 < len(args); i++ {
		if args[i] != "-i" {
			continue
		}
		if args[i+1] == value {
			return args[start:i]
		}
		start = i + 2
	}
	return nil
}

func idxOf(args []string, val string) int {
	for i, a := range args {
		if a == val {
			return i
		}
	}
	return -1
}

func assertNotContains(t *testing.T, args []string, key string) {
	t.Helper()
	for _, a := range args {
		if a == key {
			t.Errorf("args should not contain %s, got %v", key, args)
			return
		}
	}
}
