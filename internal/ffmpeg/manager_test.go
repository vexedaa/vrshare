package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindFFmpeg_FindsOnPath(t *testing.T) {
	path, err := FindFFmpeg()
	if err != nil {
		t.Skipf("FFmpeg not on PATH: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
}

func TestFindFFmpeg_ChecksCacheDir(t *testing.T) {
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "ffmpeg.exe")
	os.WriteFile(fakePath, []byte("fake"), 0755)

	path, err := findFFmpegInDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != fakePath {
		t.Errorf("expected %q, got %q", fakePath, path)
	}
}

func TestManager_SegmentDirCreated(t *testing.T) {
	dir := t.TempDir()
	segDir := filepath.Join(dir, "segments")

	m := &Manager{
		FFmpegPath: "ffmpeg",
		SegmentDir: segDir,
	}

	if err := m.EnsureSegmentDir(); err != nil {
		t.Fatalf("failed to create segment dir: %v", err)
	}

	if _, err := os.Stat(segDir); os.IsNotExist(err) {
		t.Fatal("segment dir should exist")
	}
}

func TestManager_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &Manager{
		FFmpegPath: "ffmpeg",
		SegmentDir: t.TempDir(),
	}

	err := m.Run(ctx, []string{"-version"})
	if err != nil && err != context.Canceled {
		t.Fatalf("expected nil or context.Canceled, got: %v", err)
	}
}

func TestManager_CleanupRemovesDir(t *testing.T) {
	dir := t.TempDir()
	segDir := filepath.Join(dir, "segments")
	os.MkdirAll(segDir, 0755)
	os.WriteFile(filepath.Join(segDir, "segment_0.ts"), []byte("data"), 0644)

	m := &Manager{
		FFmpegPath: "ffmpeg",
		SegmentDir: segDir,
	}

	m.Cleanup()

	if _, err := os.Stat(segDir); !os.IsNotExist(err) {
		t.Fatal("segment dir should be removed after cleanup")
	}
}

func TestManager_RestartOnCrash(t *testing.T) {
	m := &Manager{
		FFmpegPath:   "ffmpeg",
		SegmentDir:   t.TempDir(),
		MaxRestarts:  3,
		RestartDelay: 1 * time.Millisecond,
	}

	if m.restartCount != 0 {
		t.Fatalf("expected 0 restarts initially, got %d", m.restartCount)
	}

	m.recordRestart()
	if m.restartCount != 1 {
		t.Errorf("expected 1 restart, got %d", m.restartCount)
	}

	if m.shouldRestart() != true {
		t.Error("should allow restart when under max")
	}

	m.restartCount = 3
	if m.shouldRestart() != false {
		t.Error("should deny restart when at max")
	}
}
