package ffmpeg

import (
	"path/filepath"
	"testing"

	"github.com/vexedaa/vrshare/internal/config"
)

func TestBuildArgs_Defaults(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare")

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
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare")

	assertContains(t, args, "-framerate", "60")
	assertContains(t, args, "-b:v", "6000k")
	assertContains(t, args, "-g", "60")
}

func TestBuildArgs_WithResolution(t *testing.T) {
	cfg := config.Default()
	cfg.Resolution = "1280x720"
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare")

	assertContains(t, args, "-vf", "scale=1280:720")
}

func TestBuildArgs_NVENCEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare")

	assertContains(t, args, "-c:v", "h264_nvenc")
	assertContains(t, args, "-preset", "p4")
	assertContains(t, args, "-tune", "ll")
}

func TestBuildArgs_QSVEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "qsv", "/tmp/vrshare")

	assertContains(t, args, "-c:v", "h264_qsv")
	assertContains(t, args, "-preset", "veryfast")
}

func TestBuildArgs_AMFEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "amf", "/tmp/vrshare")

	assertContains(t, args, "-c:v", "h264_amf")
	assertContains(t, args, "-quality", "speed")
}

func TestBuildArgs_CPUEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare")

	assertContains(t, args, "-c:v", "libx264")
	assertContains(t, args, "-preset", "veryfast")
	assertContains(t, args, "-tune", "zerolatency")
}

func TestBuildArgs_OutputPaths(t *testing.T) {
	cfg := config.Default()
	dir := t.TempDir()
	args := BuildArgs(cfg, "cpu", dir)

	expectedSeg := filepath.Join(dir, "segment_%d.ts")
	assertContains(t, args, "-hls_segment_filename", expectedSeg)
	// last arg should be the playlist path
	expectedPlaylist := filepath.Join(dir, "stream.m3u8")
	if args[len(args)-1] != expectedPlaylist {
		t.Errorf("last arg should be playlist path, got %q", args[len(args)-1])
	}
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
