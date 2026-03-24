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

func CleanOldSegments(dir string) (int, error) {
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
		if !referenced[entry.Name()] {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err == nil {
				removed++
			}
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

func RunJanitor(ctx context.Context, dir string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed, err := CleanOldSegments(dir)
			if err != nil {
				log.Printf("janitor error: %v", err)
			} else if removed > 0 {
				log.Printf("janitor: cleaned %d old segment(s)", removed)
			}
		}
	}
}
