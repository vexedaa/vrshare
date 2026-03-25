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

	removed, err := CleanOldSegments(dir, nil)
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

	removed, err := CleanOldSegments(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed when no playlist, got %d", removed)
	}
}

func TestCleanOldSegments_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	removed, err := CleanOldSegments(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed for empty dir, got %d", removed)
	}
}

func TestCleanOldSegments_SkipsActiveSegments(t *testing.T) {
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

	// Simulate an active download of segment_1.ts
	srv := NewServer(dir)
	srv.trackStart("segment_1.ts")

	removed, err := CleanOldSegments(dir, srv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// segment_0 and segment_2 should be removed, but segment_1 should be kept (active)
	if removed != 2 {
		t.Errorf("expected 2 removed (skipping active), got %d", removed)
	}

	// segment_1 should still exist
	if _, err := os.Stat(filepath.Join(dir, "segment_1.ts")); os.IsNotExist(err) {
		t.Error("segment_1.ts should still exist (active download)")
	}

	// After ending the download, it should be cleaned up
	srv.trackEnd("segment_1.ts")
	removed, err = CleanOldSegments(dir, srv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 removed after download ended, got %d", removed)
	}
}
