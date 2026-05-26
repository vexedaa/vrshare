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

// CleanOldSegments removes .ts/.m4s files that are not in the playlist, not
// actively being downloaded, and older than minAge. The minAge gate is a
// safety net against viewers that pull the playlist and then take a moment to
// fetch the segments listed in it: without it, the janitor would race
// FFmpeg's own delete_segments + hls_delete_threshold buffer and silently
// delete segments while viewers were still downloading them.
//
// Pass minAge = 0 to skip the age check (for tests or when callers manage
// timing themselves).
func CleanOldSegments(dir string, srv *Server, minAge time.Duration) (int, error) {
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

	cutoff := time.Now().Add(-minAge)
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".ts" && ext != ".m4s" {
			continue
		}
		if referenced[entry.Name()] {
			continue
		}
		if srv != nil && srv.IsSegmentActive(entry.Name()) {
			continue
		}
		if minAge > 0 {
			info, err := entry.Info()
			if err != nil || info.ModTime().After(cutoff) {
				continue
			}
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
		if strings.HasSuffix(line, ".ts") || strings.HasSuffix(line, ".m4s") {
			segments[line] = true
		}
		// Parse EXT-X-MAP URI for init segment
		if strings.HasPrefix(line, "#EXT-X-MAP:") {
			if start := strings.Index(line, "URI=\""); start != -1 {
				start += 5
				if end := strings.Index(line[start:], "\""); end != -1 {
					segments[line[start:start+end]] = true
				}
			}
		}
		// Parse EXT-X-PART URI for partial segments
		if strings.HasPrefix(line, "#EXT-X-PART:") {
			if start := strings.Index(line, "URI=\""); start != -1 {
				start += 5
				if end := strings.Index(line[start:], "\""); end != -1 {
					segments[line[start:start+end]] = true
				}
			}
		}
	}
	return segments, scanner.Err()
}

// RunJanitor periodically cleans segments older than minAge until the context
// is cancelled. Segments referenced by the playlist or actively being
// downloaded are always preserved regardless of age.
func RunJanitor(ctx context.Context, dir string, srv *Server, interval, minAge time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed, err := CleanOldSegments(dir, srv, minAge)
			if err != nil {
				log.Printf("janitor error: %v", err)
			} else if removed > 0 {
				log.Printf("janitor: cleaned %d old segment(s)", removed)
			}
		}
	}
}
