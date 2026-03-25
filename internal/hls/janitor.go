package hls

import (
	"bufio"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CleanOldSegments removes .ts files that are not in the playlist and not
// actively being downloaded by any viewer.
func CleanOldSegments(dir string, srv *Server) (int, error) {
	playlistPath := filepath.Join(dir, "stream.m3u8")

	referenced, err := parsePlaylistSegments(playlistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	if len(referenced) == 0 {
		return 0, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".ts" {
			continue
		}
		if referenced[entry.Name()] {
			continue
		}
		if srv != nil && srv.IsSegmentActive(entry.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err == nil {
			removed++
		}
	}

	return removed, nil
}

func parsePlaylistSegments(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	segments := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasSuffix(line, ".ts") {
			segments[line] = true
		}
	}
	return segments, scanner.Err()
}

// RunJanitor periodically cleans old segments until the context is cancelled.
func RunJanitor(ctx context.Context, dir string, srv *Server, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed, err := CleanOldSegments(dir, srv)
			if err != nil {
				log.Printf("janitor error: %v", err)
			} else if removed > 0 {
				log.Printf("janitor: cleaned %d old segment(s)", removed)
			}
		}
	}
}
