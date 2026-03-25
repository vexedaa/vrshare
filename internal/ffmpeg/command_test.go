package ffmpeg

import (
	"path/filepath"
	"testing"

	"github.com/vexedaa/vrshare/internal/config"
)

func TestBuildArgs_Defaults(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false)

	assertContains(t, args, "-f", "gdigrab")
	assertContains(t, args, "-framerate", "30")
	assertContains(t, args, "-i", "desktop")
	assertContains(t, args, "-c:v", "libx264")
	assertContains(t, args, "-b:v", "4000k")
	assertContains(t, args, "-g", "30")
	assertContains(t, args, "-f", "hls")
	assertContains(t, args, "-hls_time", "1")
	assertContains(t, args, "-hls_list_size", "3")
	assertNotContains(t, args, "-vf")
}

func TestBuildArgs_CustomFPSAndBitrate(t *testing.T) {
	cfg := config.Default()
	cfg.FPS = 60
	cfg.Bitrate = 6000
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false)

	assertContains(t, args, "-framerate", "60")
	assertContains(t, args, "-b:v", "6000k")
	assertContains(t, args, "-g", "60")
}

func TestBuildArgs_WithResolution(t *testing.T) {
	cfg := config.Default()
	cfg.Resolution = "1280x720"
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false)

	assertContains(t, args, "-vf", "scale=1280:720")
}

func TestBuildArgs_NVENCEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", false)

	assertContains(t, args, "-c:v", "h264_nvenc")
	assertContains(t, args, "-preset", "p4")
	assertContains(t, args, "-tune", "ll")
}

func TestBuildArgs_QSVEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "qsv", "/tmp/vrshare", false)

	assertContains(t, args, "-c:v", "h264_qsv")
	assertContains(t, args, "-preset", "veryfast")
}

func TestBuildArgs_AMFEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "amf", "/tmp/vrshare", false)

	assertContains(t, args, "-c:v", "h264_amf")
	assertContains(t, args, "-quality", "speed")
}

func TestBuildArgs_CPUEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false)

	assertContains(t, args, "-c:v", "libx264")
	assertContains(t, args, "-preset", "veryfast")
	assertContains(t, args, "-tune", "zerolatency")
}

func TestBuildArgs_OutputPaths(t *testing.T) {
	cfg := config.Default()
	dir := t.TempDir()
	args := BuildArgs(cfg, "cpu", dir, false)

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
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true)
	assertContains(t, args, "-f", "lavfi")
	assertContains(t, args, "-i", "ddagrab=output_idx=0:framerate=30")
	assertContains(t, args, "-c:v", "h264_nvenc")
	assertNotContains(t, args, "-vf")
}

func TestBuildArgs_DDAgrab_CPUEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", true)
	assertContains(t, args, "-f", "lavfi")
	assertContains(t, args, "-i", "ddagrab=output_idx=0:framerate=30")
	assertContains(t, args, "-c:v", "libx264")
	assertContains(t, args, "-vf", "hwdownload,format=bgra,format=yuv420p")
}

func TestBuildArgs_DDAgrab_CPUEncoder_WithResolution(t *testing.T) {
	cfg := config.Default()
	cfg.Resolution = "1280x720"
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", true)
	assertContains(t, args, "-vf", "hwdownload,format=bgra,format=yuv420p,scale=1280:720")
}

func TestBuildArgs_DDAgrab_GPUEncoder_IgnoresResolution(t *testing.T) {
	cfg := config.Default()
	cfg.Resolution = "1280x720"
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true)
	assertNotContains(t, args, "-vf")
}

func TestBuildArgs_DDAgrab_MonitorIndex(t *testing.T) {
	cfg := config.Default()
	cfg.Monitor = 2
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true)
	assertContains(t, args, "-i", "ddagrab=output_idx=2:framerate=30")
}

func TestBuildArgs_DDAgrab_CustomFPS(t *testing.T) {
	cfg := config.Default()
	cfg.FPS = 60
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true)
	assertContains(t, args, "-i", "ddagrab=output_idx=0:framerate=60")
	assertNotContains(t, args, "-framerate")
}

func TestBuildArgs_AudioEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Audio = true
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true)

	assertContains(t, args, "-f", "s16le")
	assertContains(t, args, "-ar", "48000")
	assertContains(t, args, "-ac", "2")
	assertContains(t, args, "-i", "pipe:0")
	assertContains(t, args, "-c:a", "aac")
	assertContains(t, args, "-b:a", "128k")
	assertNotContains(t, args, "dshow")
}

func TestBuildArgs_AudioDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Audio = false
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true)

	assertNotContains(t, args, "s16le")
	assertNotContains(t, args, "pipe:0")
	assertNotContains(t, args, "-c:a")
}

func TestBuildArgs_GdigrabFallback(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false)
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

func assertNotContains(t *testing.T, args []string, key string) {
	t.Helper()
	for _, a := range args {
		if a == key {
			t.Errorf("args should not contain %s, got %v", key, args)
			return
		}
	}
}
