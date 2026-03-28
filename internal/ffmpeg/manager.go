package ffmpeg

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Manager struct {
	FFmpegPath   string
	SegmentDir   string
	MaxRestarts  int
	RestartDelay time.Duration
	StderrWriter io.Writer // optional: receives FFmpeg stderr output
	LogFunc      func(string) // optional: receives log messages for session log
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

// FindFFmpeg looks for ffmpeg in these locations (in order):
//  1. ~/.vrshare/ffmpeg/ (user's custom builds)
//  2. ./ffmpeg/ (bundled with release zip)
//  3. System PATH
func FindFFmpeg() (string, error) {
	// Check user cache dir first (custom builds)
	cacheDir := defaultCacheDir()
	if path, err := findFFmpegInDir(cacheDir); err == nil {
		return path, nil
	}

	// Check alongside the executable (bundled release)
	if exePath, err := os.Executable(); err == nil {
		bundleDir := filepath.Join(filepath.Dir(exePath), "ffmpeg")
		if path, err := findFFmpegInDir(bundleDir); err == nil {
			return path, nil
		}
	}

	// Fall back to PATH
	path, err := exec.LookPath("ffmpeg")
	if err == nil {
		return path, nil
	}

	return "", fmt.Errorf("ffmpeg not found in %s, ./ffmpeg/, or on PATH", cacheDir)
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
// attached as stdin so FFmpeg can read raw PCM audio from pipe:0.
func (m *Manager) Run(ctx context.Context, args []string, audioPipe *os.File) error {
	if err := m.EnsureSegmentDir(); err != nil {
		return fmt.Errorf("creating segment dir: %w", err)
	}

	m.logf("FFmpeg command: %s %s", m.FFmpegPath, strings.Join(args, " "))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Capture stderr in a buffer so we can report it on crash,
		// while also forwarding to StderrWriter for live parsing.
		var stderrBuf stderrCapture
		stderrBuf.limit = 4096
		if m.StderrWriter != nil {
			stderrBuf.forward = m.StderrWriter
		}

		m.cmd = exec.CommandContext(ctx, m.FFmpegPath, args...)
		m.cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		m.cmd.Stderr = &stderrBuf
		if audioPipe != nil {
			m.cmd.Stdin = audioPipe
		}

		log.Printf("Starting FFmpeg: %s %v", m.FFmpegPath, args)
		err := m.cmd.Run()

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			errDetail := strings.TrimSpace(stderrBuf.String())
			if errDetail != "" {
				m.logf("FFmpeg stderr: %s", errDetail)
			}
			log.Printf("FFmpeg exited with error: %v", err)
			if !m.shouldRestart() {
				return fmt.Errorf("FFmpeg crashed %d times, giving up: %w", m.restartCount, err)
			}
			m.recordRestart()
			m.logf("Restarting FFmpeg (attempt %d/%d)", m.restartCount, m.MaxRestarts)
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

func (m *Manager) logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Println(msg)
	if m.LogFunc != nil {
		m.LogFunc(msg)
	}
}

// stderrCapture captures stderr output (up to a limit) while optionally
// forwarding to another writer. Used to include stderr in error messages
// when FFmpeg crashes before producing any progress output.
type stderrCapture struct {
	mu      sync.Mutex
	buf     []byte
	limit   int
	forward io.Writer
}

func (s *stderrCapture) Write(p []byte) (int, error) {
	s.mu.Lock()
	if len(s.buf) < s.limit {
		remaining := s.limit - len(s.buf)
		n := len(p)
		if n > remaining {
			n = remaining
		}
		s.buf = append(s.buf, p[:n]...)
	}
	s.mu.Unlock()

	if s.forward != nil {
		return s.forward.Write(p)
	}
	return len(p), nil
}

func (s *stderrCapture) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.buf)
}
