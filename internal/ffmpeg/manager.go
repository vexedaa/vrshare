package ffmpeg

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

type Manager struct {
	FFmpegPath   string
	SegmentDir   string
	MaxRestarts  int
	RestartDelay time.Duration
	restartCount int
	cmd          *exec.Cmd
}

func NewManager(ffmpegPath, segmentDir string) *Manager {
	return &Manager{
		FFmpegPath:   ffmpegPath,
		SegmentDir:   segmentDir,
		MaxRestarts:  5,
		RestartDelay: 2 * time.Second,
	}
}

// FindFFmpeg looks for ffmpeg in the cache directory first (where custom
// builds with ddagrab support may live), then falls back to PATH.
func FindFFmpeg() (string, error) {
	// Check cache dir first (custom builds)
	cacheDir := defaultCacheDir()
	if path, err := findFFmpegInDir(cacheDir); err == nil {
		return path, nil
	}

	// Fall back to PATH
	path, err := exec.LookPath("ffmpeg")
	if err == nil {
		return path, nil
	}

	return "", fmt.Errorf("ffmpeg not found in %s or on PATH", cacheDir)
}

func findFFmpegInDir(dir string) (string, error) {
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name = "ffmpeg.exe"
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("ffmpeg not found in %s", dir)
}

func defaultCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".vrshare", "ffmpeg")
	}
	return filepath.Join(home, ".vrshare", "ffmpeg")
}

func (m *Manager) EnsureSegmentDir() error {
	return os.MkdirAll(m.SegmentDir, 0755)
}

// Run starts FFmpeg with the given args. If audioPipe is non-nil, it is
// attached as fd 3 (ExtraFiles) so FFmpeg can read raw PCM audio from pipe:3.
func (m *Manager) Run(ctx context.Context, args []string, audioPipe *os.File) error {
	if err := m.EnsureSegmentDir(); err != nil {
		return fmt.Errorf("creating segment dir: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		m.cmd = exec.CommandContext(ctx, m.FFmpegPath, args...)
		m.cmd.Stdout = os.Stdout
		m.cmd.Stderr = os.Stderr
		if audioPipe != nil {
			m.cmd.ExtraFiles = []*os.File{audioPipe} // fd 3
		}

		log.Printf("Starting FFmpeg: %s %v", m.FFmpegPath, args)
		err := m.cmd.Run()

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			log.Printf("FFmpeg exited with error: %v", err)
			if !m.shouldRestart() {
				return fmt.Errorf("FFmpeg crashed %d times, giving up: %w", m.restartCount, err)
			}
			m.recordRestart()
			log.Printf("Restarting FFmpeg in %v (restart %d/%d)", m.RestartDelay, m.restartCount, m.MaxRestarts)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(m.RestartDelay):
			}
			continue
		}

		return nil
	}
}

func (m *Manager) shouldRestart() bool {
	return m.restartCount < m.MaxRestarts
}

func (m *Manager) recordRestart() {
	m.restartCount++
}

func (m *Manager) Stop() {
	if m.cmd != nil && m.cmd.Process != nil {
		m.cmd.Process.Kill()
	}
}

func (m *Manager) Cleanup() {
	os.RemoveAll(m.SegmentDir)
}
