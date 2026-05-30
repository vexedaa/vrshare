package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vexedaa/vrshare/internal/config"
)

// TestLogBufferBounded verifies the in-memory event log is capped at
// maxLogEntries, keeps the most recent lines, and that LogSeq counts every
// line ever logged (not just the retained ones).
func TestLogBufferBounded(t *testing.T) {
	s := New(config.Default())

	total := maxLogEntries + 250
	for i := 0; i < total; i++ {
		s.log(fmt.Sprintf("line %d", i))
	}

	entries := s.LogEntries()
	if len(entries) != maxLogEntries {
		t.Fatalf("log buffer not capped: got %d entries, want %d", len(entries), maxLogEntries)
	}

	wantFirst := fmt.Sprintf("line %d", total-maxLogEntries)
	if entries[0].Message != wantFirst {
		t.Errorf("oldest retained = %q, want %q", entries[0].Message, wantFirst)
	}
	wantLast := fmt.Sprintf("line %d", total-1)
	if entries[len(entries)-1].Message != wantLast {
		t.Errorf("newest retained = %q, want %q", entries[len(entries)-1].Message, wantLast)
	}

	if seq := s.LogSeq(); seq != uint64(total) {
		t.Errorf("LogSeq = %d, want %d (must count every line, including dropped)", seq, total)
	}
}

// TestCleanupStaleSegmentDirs verifies only stale vrshare-segments-* dirs are
// removed: fresh dirs, the current (keep) dir, and unrelated dirs are spared.
func TestCleanupStaleSegmentDirs(t *testing.T) {
	tmp := t.TempDir()

	mk := func(name string, age time.Duration) string {
		p := filepath.Join(tmp, name)
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(p, "segment_0.ts"), []byte("x"), 0644)
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		return p
	}

	old := mk("vrshare-segments-old", time.Hour)        // stale -> remove
	fresh := mk("vrshare-segments-fresh", time.Second)  // recently written -> keep
	current := mk("vrshare-segments-current", time.Hour) // stale but is the keep dir -> keep
	other := mk("some-other-dir", time.Hour)            // wrong prefix -> keep

	removed := cleanupStaleSegmentDirs(tmp, current, 10*time.Minute)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("stale segment dir should have been removed")
	}
	for _, p := range []string{fresh, current, other} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should still exist: %v", filepath.Base(p), err)
		}
	}
}
