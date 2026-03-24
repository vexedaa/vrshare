package hls

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanOldSegments_RemovesOldFiles(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i <= 5; i++ {
		name := filepath.Join(dir, fmt.Sprintf("segment_%d.ts", i))
		os.WriteFile(name, []byte("data"), 0644)
	}

	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n" +
		"#EXTINF:1.0,\nsegment_3.ts\n" +
		"#EXTINF:1.0,\nsegment_4.ts\n" +
		"#EXTINF:1.0,\nsegment_5.ts\n"
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte(playlist), 0644)

	removed, err := CleanOldSegments(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if removed != 3 {
		t.Errorf("expected 3 removed, got %d", removed)
	}

	for i := 0; i <= 2; i++ {
		path := filepath.Join(dir, fmt.Sprintf("segment_%d.ts", i))
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("segment_%d.ts should have been removed", i)
		}
	}

	for i := 3; i <= 5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("segment_%d.ts", i))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("segment_%d.ts should still exist", i)
		}
	}
}

func TestCleanOldSegments_NoPlaylist(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "segment_0.ts"), []byte("data"), 0644)

	removed, err := CleanOldSegments(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed when no playlist, got %d", removed)
	}
}

func TestCleanOldSegments_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	removed, err := CleanOldSegments(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed for empty dir, got %d", removed)
	}
}
