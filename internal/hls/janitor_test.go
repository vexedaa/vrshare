package hls

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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

	removed, err := CleanOldSegments(dir, nil, 0)
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

	removed, err := CleanOldSegments(dir, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed when no playlist, got %d", removed)
	}
}

func TestCleanOldSegments_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	removed, err := CleanOldSegments(dir, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed for empty dir, got %d", removed)
	}
}

func TestCleanOldSegments_RemovesFMP4Files(t *testing.T) {
	dir := t.TempDir()

	// Create init.mp4 and m4s segments
	os.WriteFile(filepath.Join(dir, "init.mp4"), []byte("init"), 0644)
	for i := 0; i <= 5; i++ {
		name := filepath.Join(dir, fmt.Sprintf("segment_%d.m4s", i))
		os.WriteFile(name, []byte("data"), 0644)
	}

	// Playlist references segments 3-5
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n" +
		"#EXT-X-MAP:URI=\"init.mp4\"\n" +
		"#EXTINF:0.5,\nsegment_3.m4s\n" +
		"#EXTINF:0.5,\nsegment_4.m4s\n" +
		"#EXTINF:0.5,\nsegment_5.m4s\n"
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte(playlist), 0644)

	removed, err := CleanOldSegments(dir, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if removed != 3 {
		t.Errorf("expected 3 removed, got %d", removed)
	}

	// init.mp4 should NOT be removed (referenced via EXT-X-MAP)
	if _, err := os.Stat(filepath.Join(dir, "init.mp4")); os.IsNotExist(err) {
		t.Error("init.mp4 should not be removed")
	}

	// Referenced segments should still exist
	for i := 3; i <= 5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("segment_%d.m4s", i))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("segment_%d.m4s should still exist", i)
		}
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

	removed, err := CleanOldSegments(dir, srv, 0)
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
	removed, err = CleanOldSegments(dir, srv, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 removed after download ended, got %d", removed)
	}
}

func TestCleanOldSegments_RespectsMinAge(t *testing.T) {
	dir := t.TempDir()

	// Two segments not in the playlist: one old, one freshly written.
	oldSeg := filepath.Join(dir, "segment_0.ts")
	newSeg := filepath.Join(dir, "segment_1.ts")
	os.WriteFile(oldSeg, []byte("data"), 0644)
	os.WriteFile(newSeg, []byte("data"), 0644)
	// Backdate the old one by 1 minute.
	past := time.Now().Add(-1 * time.Minute)
	if err := os.Chtimes(oldSeg, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Playlist references neither segment, but the minAge gate must keep
	// the fresh one alive (it could still be downloading even though no
	// active read is registered yet).
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1.0,\nsegment_99.ts\n"
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte(playlist), 0644)

	removed, err := CleanOldSegments(dir, nil, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 removed (old only), got %d", removed)
	}
	if _, err := os.Stat(oldSeg); !os.IsNotExist(err) {
		t.Error("segment_0.ts (old) should have been removed")
	}
	if _, err := os.Stat(newSeg); err != nil {
		t.Error("segment_1.ts (new) should still exist")
	}
}
