# VRShare GUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Wails-based GUI with system tray to VRShare, preserving the existing CLI behavior when launched with arguments.

**Architecture:** Unified binary with mode detection. New `internal/server/` orchestrator extracts startup logic from main.go. New `internal/gui/` package wraps Wails. Svelte + Tailwind frontend in `frontend/`. Existing internal packages stay Wails-free with minor targeted extensions.

**Tech Stack:** Go 1.24, Wails v2, Svelte, Tailwind CSS, Windows APIs (GetConsoleWindow, Shell_NotifyIcon)

**Spec:** `docs/superpowers/specs/2026-03-25-gui-design.md`

---

## File Structure

### New Files

```
internal/server/
  server.go          — Server struct: New, Start, Stop, State, Config
  stats.go           — StatsParser: io.Writer that parses FFmpeg stderr progress lines
  persistence.go     — Load/save config.json, settings.json, preset files
  types.go           — StreamState, AppSettings, Preset, SystemInfo types
  stats_test.go      — Tests for FFmpeg output parsing
  persistence_test.go — Tests for config/preset save/load

internal/gui/
  app.go             — Wails App struct, bindings, event emitter
  tray.go            — System tray setup (icon states, menu, actions)
  detect.go          — DetectSystem: probe encoders, monitors, audio devices

cmd/vrshare/
  mode_windows.go    — hasConsole() via GetConsoleWindow, launchGUI()
  mode_other.go      — stub: hasConsole() always true, launchGUI() no-op

frontend/
  package.json       — Node dependencies (svelte, tailwind, vite, wails)
  vite.config.js     — Vite config for Wails
  tailwind.config.js — Tailwind config
  postcss.config.js  — PostCSS with Tailwind plugin
  index.html         — Vite entry point
  src/
    main.js          — Svelte app mount
    App.svelte       — Root: routes between Wizard, Dashboard, Settings
    lib/
      Wizard.svelte    — First-run wizard (auto-detect + confirm)
      Dashboard.svelte — Dashboard with idle/streaming states
      Settings.svelte  — Full settings view
      StatsRow.svelte  — Encoding stats display row
      EventLog.svelte  — Scrolling timestamped log feed
      PresetPicker.svelte — Preset dropdown + save/delete

wails.json           — Wails project configuration
```

### Modified Files

```
internal/config/config.go       — Add Tunnel, AudioDevice fields; add JSON tags
internal/config/config_test.go  — Tests for new fields
internal/ffmpeg/manager.go      — Accept optional stats io.Writer for stderr
internal/hls/server.go          — Add viewer count tracking
cmd/vrshare/main.go             — Mode detection, delegate to server/ or gui/
go.mod                          — Add Wails dependency
.gitignore                      — Add frontend/node_modules/, frontend/dist/
```

---

## Phase 1: Backend Foundation

### Task 1: Extend config.Config

Add Tunnel and AudioDevice fields to the config struct, plus JSON tags for serialization.

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Update Config struct with new fields and JSON tags**

In `internal/config/config.go`, add `Tunnel` and `AudioDevice` fields to the struct, and add `json` tags to all fields:

```go
type Config struct {
	Port       int         `json:"port"`
	Monitor    int         `json:"monitor"`
	FPS        int         `json:"fps"`
	Resolution string      `json:"resolution"`
	Bitrate    int         `json:"bitrate"`
	Encoder    EncoderType `json:"encoder"`
	Audio      bool        `json:"audio"`
	AudioDevice string    `json:"audioDevice"`
	Tunnel     string      `json:"tunnel"`
}
```

Update `Default()` to include the new fields:
```go
func Default() Config {
	return Config{
		Port:    8080,
		FPS:     30,
		Bitrate: 4000,
		Encoder: EncoderAuto,
		Audio:   false,
		Tunnel:  "",
	}
}
```

Add validation for Tunnel in `Validate()`:
```go
if c.Tunnel != "" && c.Tunnel != "cloudflare" && c.Tunnel != "tailscale" {
	return fmt.Errorf("tunnel must be empty, \"cloudflare\", or \"tailscale\"")
}
```

- [ ] **Step 2: Add tests for new fields**

In `internal/config/config_test.go`, add test cases:

```go
func TestValidateTunnel(t *testing.T) {
	c := Default()
	c.Tunnel = "cloudflare"
	if err := c.Validate(); err != nil {
		t.Fatalf("cloudflare tunnel should be valid: %v", err)
	}
	c.Tunnel = "tailscale"
	if err := c.Validate(); err != nil {
		t.Fatalf("tailscale tunnel should be valid: %v", err)
	}
	c.Tunnel = "invalid"
	if err := c.Validate(); err == nil {
		t.Fatal("invalid tunnel should fail validation")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/config/ -v`
Expected: All tests pass including new tunnel validation tests.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add Tunnel and AudioDevice fields to config"
```

---

### Task 2: FFmpeg Stats Parser

Create a `StatsParser` that wraps an `io.Writer` and extracts encoding stats (fps, bitrate, speed, drop_frames) from FFmpeg's stderr output.

FFmpeg stderr progress lines look like:
```
frame=  120 fps= 60 q=28.0 size=     384kB time=00:00:02.00 bitrate=1571.1kbits/s speed=1.00x
```

**Files:**
- Create: `internal/server/stats.go`
- Create: `internal/server/stats_test.go`

- [ ] **Step 1: Write tests for stats parsing**

Create `internal/server/stats_test.go`:

```go
package server

import (
	"testing"
)

func TestParseStatsLine(t *testing.T) {
	line := "frame=  120 fps= 60.0 q=28.0 size=     384kB time=00:00:02.00 bitrate=1571.1kbits/s speed=1.00x"
	stats := parseStatsLine(line)

	if stats.FPS != 60.0 {
		t.Errorf("FPS = %f, want 60.0", stats.FPS)
	}
	if stats.Bitrate != 1571 {
		t.Errorf("Bitrate = %d, want 1571", stats.Bitrate)
	}
	if stats.Speed != 1.0 {
		t.Errorf("Speed = %f, want 1.0", stats.Speed)
	}
}

func TestParseStatsLinePartial(t *testing.T) {
	line := "Press [q] to stop, [?] for help"
	stats := parseStatsLine(line)
	if stats.FPS != 0 {
		t.Errorf("non-progress line should return zero stats")
	}
}

func TestStatsParserWrite(t *testing.T) {
	p := NewStatsParser(nil)
	input := "frame=  240 fps= 59.8 q=25.0 size=     768kB time=00:00:04.00 bitrate=1573.2kbits/s speed=0.98x\n"
	p.Write([]byte(input))

	s := p.Latest()
	if s.FPS != 59.8 {
		t.Errorf("FPS = %f, want 59.8", s.FPS)
	}
	if s.Speed != 0.98 {
		t.Errorf("Speed = %f, want 0.98", s.Speed)
	}
}

func TestStatsParserPassthrough(t *testing.T) {
	var buf []byte
	w := &sliceWriter{buf: &buf}
	p := NewStatsParser(w)
	input := []byte("some output\n")
	n, err := p.Write(input)
	if err != nil || n != len(input) {
		t.Errorf("passthrough failed: n=%d err=%v", n, err)
	}
	if string(*w.buf) != string(input) {
		t.Errorf("passthrough data mismatch")
	}
}

type sliceWriter struct{ buf *[]byte }

func (w *sliceWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -v -run TestParse`
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement StatsParser**

Create `internal/server/stats.go`:

```go
package server

import (
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// EncodingStats holds parsed FFmpeg encoding statistics.
type EncodingStats struct {
	FPS           float64
	Bitrate       int     // kbps
	DroppedFrames int
	Speed         float64 // encoding speed multiplier (1.0 = realtime)
}

// StatsParser wraps an io.Writer and parses FFmpeg stderr progress lines.
// If inner is nil, stderr output is discarded after parsing.
type StatsParser struct {
	inner  io.Writer
	mu     sync.Mutex
	latest EncodingStats
	buf    []byte
}

func NewStatsParser(inner io.Writer) *StatsParser {
	return &StatsParser{inner: inner}
}

func (p *StatsParser) Write(data []byte) (int, error) {
	// Buffer data and process complete lines
	p.buf = append(p.buf, data...)
	for {
		idx := indexOf(p.buf, '\n')
		if idx < 0 {
			// Also check for \r (FFmpeg uses \r for progress updates)
			idx = indexOf(p.buf, '\r')
			if idx < 0 {
				break
			}
		}
		line := string(p.buf[:idx])
		p.buf = p.buf[idx+1:]

		stats := parseStatsLine(line)
		if stats.FPS > 0 || stats.Bitrate > 0 {
			p.mu.Lock()
			p.latest = stats
			p.mu.Unlock()
		}
	}

	if p.inner != nil {
		return p.inner.Write(data)
	}
	return len(data), nil
}

func (p *StatsParser) Latest() EncodingStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.latest
}

var (
	reFPS     = regexp.MustCompile(`fps=\s*([\d.]+)`)
	reBitrate = regexp.MustCompile(`bitrate=\s*([\d.]+)kbits/s`)
	reSpeed   = regexp.MustCompile(`speed=\s*([\d.]+)x`)
	reDrop    = regexp.MustCompile(`drop=\s*(\d+)`)
)

func parseStatsLine(line string) EncodingStats {
	if !strings.Contains(line, "frame=") {
		return EncodingStats{}
	}
	var s EncodingStats
	if m := reFPS.FindStringSubmatch(line); len(m) > 1 {
		s.FPS, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reBitrate.FindStringSubmatch(line); len(m) > 1 {
		f, _ := strconv.ParseFloat(m[1], 64)
		s.Bitrate = int(f)
	}
	if m := reSpeed.FindStringSubmatch(line); len(m) > 1 {
		s.Speed, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reDrop.FindStringSubmatch(line); len(m) > 1 {
		s.DroppedFrames, _ = strconv.Atoi(m[1])
	}
	return s
}

func indexOf(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -v -run TestParse`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/stats.go internal/server/stats_test.go
git commit -m "feat: add FFmpeg stderr stats parser"
```

---

### Task 3: Add Viewer Count Tracking to HLS Server

Add a lightweight counter to track active connections to the HLS playlist endpoint.

**Files:**
- Modify: `internal/hls/server.go`
- Modify: `internal/hls/server_test.go`

- [ ] **Step 1: Add viewer tracking to Server struct**

In `internal/hls/server.go`, add a viewer count field and accessor. Add to the `Server` struct:

```go
type Server struct {
	dir             string
	mu              sync.Mutex
	activeDownloads map[string]int
	ffmpegPath      string
	mp4Port         int
	viewerCount     int32 // atomic
}
```

Add import for `sync/atomic` and add accessor method:

```go
func (s *Server) ViewerCount() int {
	return int(atomic.LoadInt32(&s.viewerCount))
}
```

In `ServeHTTP`, around the `.m3u8` serving path, track viewers. When a client requests the playlist, increment the counter and set a brief window to decrement (viewers poll every segment duration). Add before the existing m3u8 file serving:

```go
if strings.HasSuffix(r.URL.Path, ".m3u8") {
	atomic.AddInt32(&s.viewerCount, 1)
	// Decrement after typical HLS poll interval (segment duration + margin)
	go func() {
		time.Sleep(3 * time.Second)
		atomic.AddInt32(&s.viewerCount, -1)
	}()
	// ... existing m3u8 serving code ...
}
```

- [ ] **Step 2: Add test for viewer count**

In `internal/hls/server_test.go`, add:

```go
func TestViewerCount(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte("#EXTM3U\n"), 0644)
	srv := NewServer(dir)

	if srv.ViewerCount() != 0 {
		t.Fatalf("initial viewer count should be 0")
	}

	req := httptest.NewRequest("GET", "/stream.m3u8", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if srv.ViewerCount() != 1 {
		t.Fatalf("viewer count should be 1 after playlist request, got %d", srv.ViewerCount())
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/hls/ -v`
Expected: All tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/hls/server.go internal/hls/server_test.go
git commit -m "feat: add viewer count tracking to HLS server"
```

---

### Task 4: Config/Settings/Preset Persistence

Create persistence layer for loading and saving config, app settings, and presets to `~/.vrshare/`.

**Files:**
- Create: `internal/server/types.go`
- Create: `internal/server/persistence.go`
- Create: `internal/server/persistence_test.go`

- [ ] **Step 1: Create types**

Create `internal/server/types.go`:

```go
package server

import (
	"time"

	"github.com/vexedaa/vrshare/internal/config"
)

// StreamState represents the current state of the streaming server.
type StreamState struct {
	Status        string        `json:"status"` // "idle", "starting", "streaming", "error"
	Error         string        `json:"error"`
	Uptime        time.Duration `json:"uptime"`
	StreamURL     string        `json:"streamURL"`
	FPS           float64       `json:"fps"`
	Bitrate       int           `json:"bitrate"`
	DroppedFrames int           `json:"droppedFrames"`
	Speed         float64       `json:"speed"`
	ViewerCount   int           `json:"viewerCount"`
}

// AppSettings holds application-level preferences.
type AppSettings struct {
	FirstRunComplete bool   `json:"firstRunComplete"`
	CloseBehavior    string `json:"closeBehavior"` // "tray" or "quit"
}

func DefaultSettings() AppSettings {
	return AppSettings{
		FirstRunComplete: false,
		CloseBehavior:    "tray",
	}
}

// Preset is a named configuration snapshot.
type Preset struct {
	Name   string        `json:"name"`
	Config config.Config `json:"config"`
}

// DefaultPreset returns the default preset created on first run.
func DefaultPreset() Preset {
	return Preset{
		Name: "Default",
		Config: config.Config{
			Port:     8080,
			FPS:      60,
			Bitrate:  4000,
			Encoder:  config.EncoderAuto,
			Audio:    true,
			Resolution: "1920x1080",
		},
	}
}

// SystemInfo holds detected system capabilities.
type SystemInfo struct {
	Encoders     []EncoderInfo `json:"encoders"`
	Monitors     []MonitorInfo `json:"monitors"`
	AudioDevices []AudioDevice `json:"audioDevices"`
}

type EncoderInfo struct {
	Name      string `json:"name"`      // e.g. "h264_nvenc"
	Type      string `json:"type"`      // "nvenc", "qsv", "amf", "cpu"
	Label     string `json:"label"`     // human-readable, e.g. "NVIDIA NVENC"
	Available bool   `json:"available"`
}

type MonitorInfo struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	Resolution string `json:"resolution"` // "2560x1440"
	IsPrimary  bool   `json:"isPrimary"`
}

type AudioDevice struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}
```

- [ ] **Step 2: Write persistence tests**

Create `internal/server/persistence_test.go`:

```go
package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vexedaa/vrshare/internal/config"
)

func TestSaveAndLoadConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Port = 9090
	cfg.FPS = 60

	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.Port != 9090 || loaded.FPS != 60 {
		t.Errorf("loaded config mismatch: %+v", loaded)
	}
}

func TestLoadConfigDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig on empty dir: %v", err)
	}
	if cfg.Port != config.Default().Port {
		t.Errorf("expected default config when file missing")
	}
}

func TestSaveAndLoadSettings(t *testing.T) {
	dir := t.TempDir()
	s := AppSettings{FirstRunComplete: true, CloseBehavior: "quit"}

	if err := SaveSettings(dir, s); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	loaded, err := LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if !loaded.FirstRunComplete || loaded.CloseBehavior != "quit" {
		t.Errorf("loaded settings mismatch: %+v", loaded)
	}
}

func TestLoadSettingsDefault(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings on empty dir: %v", err)
	}
	if s.FirstRunComplete != false || s.CloseBehavior != "tray" {
		t.Errorf("expected default settings when file missing")
	}
}

func TestPresetCRUD(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Port = 7070

	// Save
	if err := SavePreset(dir, "My Preset", cfg); err != nil {
		t.Fatalf("SavePreset: %v", err)
	}

	// List
	presets, err := ListPresets(dir)
	if err != nil {
		t.Fatalf("ListPresets: %v", err)
	}
	if len(presets) != 1 || presets[0].Name != "My Preset" {
		t.Errorf("expected 1 preset named 'My Preset', got %+v", presets)
	}

	// Load
	loaded, err := LoadPreset(dir, "My Preset")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	if loaded.Port != 7070 {
		t.Errorf("loaded preset port = %d, want 7070", loaded.Port)
	}

	// Delete
	if err := DeletePreset(dir, "My Preset"); err != nil {
		t.Fatalf("DeletePreset: %v", err)
	}
	presets, _ = ListPresets(dir)
	if len(presets) != 0 {
		t.Errorf("expected 0 presets after delete, got %d", len(presets))
	}
}

func TestPresetSanitizeName(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()

	// Should handle special characters in name
	if err := SavePreset(dir, "My/Bad\\Name", cfg); err != nil {
		t.Fatalf("SavePreset with special chars: %v", err)
	}

	presets, _ := ListPresets(dir)
	if len(presets) != 1 {
		t.Errorf("expected 1 preset, got %d", len(presets))
	}
}

func TestDataDir(t *testing.T) {
	d, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if !filepath.IsAbs(d) {
		t.Errorf("DataDir should return absolute path, got %s", d)
	}
	// Should end with .vrshare
	if filepath.Base(d) != ".vrshare" {
		t.Errorf("DataDir should end with .vrshare, got %s", d)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/server/ -v -run TestSave`
Expected: FAIL — functions not defined yet.

- [ ] **Step 4: Implement persistence**

Create `internal/server/persistence.go`:

```go
package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/vexedaa/vrshare/internal/config"
)

// DataDir returns the path to ~/.vrshare/, creating it if needed.
func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".vrshare")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// SaveConfig writes config to dir/config.json.
func SaveConfig(dir string, cfg config.Config) error {
	return writeJSON(filepath.Join(dir, "config.json"), cfg)
}

// LoadConfig reads config from dir/config.json.
// Returns Default() if file doesn't exist.
func LoadConfig(dir string) (config.Config, error) {
	var cfg config.Config
	err := readJSON(filepath.Join(dir, "config.json"), &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return config.Default(), nil
	}
	return cfg, err
}

// SaveSettings writes app settings to dir/settings.json.
func SaveSettings(dir string, s AppSettings) error {
	return writeJSON(filepath.Join(dir, "settings.json"), s)
}

// LoadSettings reads app settings from dir/settings.json.
// Returns DefaultSettings() if file doesn't exist.
func LoadSettings(dir string) (AppSettings, error) {
	var s AppSettings
	err := readJSON(filepath.Join(dir, "settings.json"), &s)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultSettings(), nil
	}
	return s, err
}

// SavePreset saves a named preset to dir/presets/<name>.json.
func SavePreset(dir, name string, cfg config.Config) error {
	presetsDir := filepath.Join(dir, "presets")
	if err := os.MkdirAll(presetsDir, 0755); err != nil {
		return err
	}
	p := Preset{Name: name, Config: cfg}
	return writeJSON(filepath.Join(presetsDir, sanitizeName(name)+".json"), p)
}

// LoadPreset loads a preset by name from dir/presets/<name>.json.
func LoadPreset(dir, name string) (config.Config, error) {
	var p Preset
	err := readJSON(filepath.Join(dir, "presets", sanitizeName(name)+".json"), &p)
	return p.Config, err
}

// ListPresets returns all presets in dir/presets/.
func ListPresets(dir string) ([]Preset, error) {
	presetsDir := filepath.Join(dir, "presets")
	entries, err := os.ReadDir(presetsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var presets []Preset
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var p Preset
		if err := readJSON(filepath.Join(presetsDir, e.Name()), &p); err == nil {
			presets = append(presets, p)
		}
	}
	return presets, nil
}

// DeletePreset removes a preset by name.
func DeletePreset(dir, name string) error {
	return os.Remove(filepath.Join(dir, "presets", sanitizeName(name)+".json"))
}

func sanitizeName(name string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return r.Replace(name)
}

func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/server/ -v`
Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server/types.go internal/server/persistence.go internal/server/persistence_test.go
git commit -m "feat: add config/settings/preset persistence layer"
```

---

### Task 5: FFmpeg Manager Stats Hook

Modify `ffmpeg/manager.go` to accept an optional `io.Writer` for stderr, allowing the stats parser to intercept FFmpeg output.

**Files:**
- Modify: `internal/ffmpeg/manager.go`
- Modify: `internal/ffmpeg/manager_test.go`

- [ ] **Step 1: Add StderrWriter field to Manager**

In `internal/ffmpeg/manager.go`, add a field to the `Manager` struct:

```go
type Manager struct {
	FFmpegPath   string
	SegmentDir   string
	MaxRestarts  int
	RestartDelay time.Duration
	StderrWriter io.Writer // optional: receives FFmpeg stderr output
}
```

Add `"io"` to imports.

In `NewManager`, keep defaults as-is (StderrWriter defaults to nil).

In the `Run` method, where stderr is currently set to `os.Stderr`, change to:

```go
if m.StderrWriter != nil {
	cmd.Stderr = m.StderrWriter
} else {
	cmd.Stderr = os.Stderr
}
```

- [ ] **Step 2: Run existing tests to verify no regressions**

Run: `go test ./internal/ffmpeg/ -v`
Expected: All existing tests still pass.

- [ ] **Step 3: Commit**

```bash
git add internal/ffmpeg/manager.go
git commit -m "feat: add StderrWriter hook to FFmpeg manager"
```

---

### Task 6: Server Orchestrator

Create `internal/server/server.go` — the main orchestrator that wires together FFmpeg, HLS, audio, and tunnel. Extracts the startup logic from `main.go`.

**Files:**
- Create: `internal/server/server.go`

- [ ] **Step 1: Implement Server**

Create `internal/server/server.go`:

```go
package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/vexedaa/vrshare/internal/config"
	"github.com/vexedaa/vrshare/internal/ffmpeg"
	"github.com/vexedaa/vrshare/internal/hls"
	"github.com/vexedaa/vrshare/internal/tunnel"
)

type Server struct {
	cfg         config.Config
	mu          sync.Mutex
	status      string
	startTime   time.Time
	streamURL   string
	errMsg      string
	cancel      context.CancelFunc
	done        chan struct{}
	hlsSrv      *hls.Server
	stats       *StatsParser
	ffmpegPath  string
	useDDAgrab  bool
	encoder     string
	logEntries  []LogEntry
	logMu       sync.Mutex
}

type LogEntry struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

func New(cfg config.Config) *Server {
	return &Server{
		cfg:    cfg,
		status: "idle",
	}
}

func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.status == "streaming" || s.status == "starting" {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.status = "starting"
	s.errMsg = ""
	s.logEntries = nil
	s.mu.Unlock()

	s.log("Starting server...")

	// Find FFmpeg
	ffmpegPath, err := ffmpeg.FindFFmpeg()
	if err != nil {
		s.setError("FFmpeg not found: " + err.Error())
		return err
	}
	s.ffmpegPath = ffmpegPath
	s.log("FFmpeg found: " + ffmpegPath)

	// Probe encoder
	probe := ffmpeg.ProbeFFmpegEncoder(ffmpegPath)
	s.encoder = ffmpeg.ResolveEncoder(string(s.cfg.Encoder), probe)
	s.useDDAgrab = ffmpeg.ProbeDDAgrab(ffmpegPath)
	s.log(fmt.Sprintf("Encoder: %s, DDAgrab: %v", s.encoder, s.useDDAgrab))

	// Create temp segment directory
	segDir, err := os.MkdirTemp("", "vrshare-segments-*")
	if err != nil {
		s.setError("Failed to create segment dir: " + err.Error())
		return err
	}

	// Start HLS server
	s.hlsSrv = hls.NewServer(segDir)
	s.hlsSrv.SetMP4Support(ffmpegPath, s.cfg.Port)
	httpSrv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.cfg.Port),
		Handler: s.hlsSrv,
	}

	go func() {
		if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Build stream URL
	ip := getOutboundIP()
	s.mu.Lock()
	s.streamURL = fmt.Sprintf("http://%s:%d/stream.m3u8", ip, s.cfg.Port)
	s.mu.Unlock()
	s.log("Stream URL: " + s.streamURL)

	// Start janitor
	srvCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})

	go hls.RunJanitor(srvCtx, segDir, s.hlsSrv, 5*time.Second)

	// Start audio if enabled
	var audioPipe *os.File
	if s.cfg.Audio {
		s.log("Audio capture enabled")
		// Audio setup follows existing main.go pattern
		// Create pipe, start capturer writing to pipe
		r, w, err := os.Pipe()
		if err != nil {
			cancel()
			s.setError("Failed to create audio pipe: " + err.Error())
			return err
		}
		audioPipe = r

		// Import and start audio capturer
		ac := newAudioCapturer(w)
		go ac.Start(srvCtx)
	}

	// Start tunnel if configured
	if s.cfg.Tunnel != "" {
		s.log("Starting tunnel: " + s.cfg.Tunnel)
		tun, err := tunnel.Start(srvCtx, s.cfg.Tunnel, s.cfg.Port)
		if err != nil {
			s.log("Tunnel warning: " + err.Error())
		} else {
			s.mu.Lock()
			s.streamURL = tun.StreamURL()
			s.mu.Unlock()
			s.log("Tunnel URL: " + s.streamURL)
		}
	}

	// Build FFmpeg args and start
	args := ffmpeg.BuildArgs(s.cfg, s.encoder, segDir, s.useDDAgrab)
	mgr := ffmpeg.NewManager(ffmpegPath, segDir)

	// Hook stats parser
	s.stats = NewStatsParser(os.Stderr)
	mgr.StderrWriter = s.stats

	s.mu.Lock()
	s.status = "streaming"
	s.startTime = time.Now()
	s.mu.Unlock()
	s.log("Stream started")

	// Run FFmpeg in background
	go func() {
		defer close(s.done)
		if err := mgr.Run(srvCtx, args, audioPipe); err != nil {
			s.setError("FFmpeg error: " + err.Error())
		}
		httpSrv.Shutdown(context.Background())
		mgr.Cleanup()
		s.mu.Lock()
		if s.status != "error" {
			s.status = "idle"
		}
		s.mu.Unlock()
	}()

	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	if s.status != "streaming" && s.status != "starting" {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	s.log("Stopping stream...")
	if s.cancel != nil {
		s.cancel()
	}
	if s.done != nil {
		<-s.done
	}
	s.mu.Lock()
	s.status = "idle"
	s.mu.Unlock()
	s.log("Stream stopped")
	return nil
}

func (s *Server) State() StreamState {
	s.mu.Lock()
	state := StreamState{
		Status:    s.status,
		Error:     s.errMsg,
		StreamURL: s.streamURL,
	}
	if s.status == "streaming" {
		state.Uptime = time.Since(s.startTime)
	}
	s.mu.Unlock()

	if s.stats != nil {
		es := s.stats.Latest()
		state.FPS = es.FPS
		state.Bitrate = es.Bitrate
		state.DroppedFrames = es.DroppedFrames
		state.Speed = es.Speed
	}
	if s.hlsSrv != nil {
		state.ViewerCount = s.hlsSrv.ViewerCount()
	}

	return state
}

func (s *Server) Config() config.Config {
	return s.cfg
}

func (s *Server) SetConfig(cfg config.Config) {
	s.cfg = cfg
}

func (s *Server) LogEntries() []LogEntry {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	entries := make([]LogEntry, len(s.logEntries))
	copy(entries, s.logEntries)
	return entries
}

func (s *Server) setError(msg string) {
	s.mu.Lock()
	s.status = "error"
	s.errMsg = msg
	s.mu.Unlock()
	s.log("Error: " + msg)
}

func (s *Server) log(msg string) {
	s.logMu.Lock()
	s.logEntries = append(s.logEntries, LogEntry{
		Time:    time.Now(),
		Message: msg,
	})
	s.logMu.Unlock()
	log.Println(msg)
}

// newAudioCapturer creates an audio capturer (platform-specific).
// This is a thin wrapper to avoid importing audio package on non-Windows.
// On Windows, this calls audio.NewCapturer(w).
// Defined in server_audio_windows.go / server_audio_other.go.

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "localhost"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
```

- [ ] **Step 2: Create platform-specific audio wrappers**

Create `internal/server/audio_windows.go`:

```go
//go:build windows

package server

import (
	"io"

	"github.com/vexedaa/vrshare/internal/audio"
)

type audioCapturer struct {
	c *audio.Capturer
}

func newAudioCapturer(w io.Writer) *audioCapturer {
	return &audioCapturer{c: audio.NewCapturer(w)}
}

func (a *audioCapturer) Start(ctx context.Context) {
	a.c.Start(ctx)
}
```

Create `internal/server/audio_other.go`:

```go
//go:build !windows

package server

import (
	"context"
	"io"
	"log"
)

type audioCapturer struct{}

func newAudioCapturer(w io.Writer) *audioCapturer {
	return &audioCapturer{}
}

func (a *audioCapturer) Start(ctx context.Context) {
	log.Println("Audio capture not supported on this platform")
}
```

- [ ] **Step 3: Verify the package compiles**

Run: `go build ./internal/server/`
Expected: Compiles without errors.

- [ ] **Step 4: Commit**

```bash
git add internal/server/server.go internal/server/audio_windows.go internal/server/audio_other.go
git commit -m "feat: add server orchestrator package"
```

---

### Task 7: Refactor main.go to Use Server Orchestrator

Replace the inline orchestration in `main.go` with calls to `server.Server`, keeping identical CLI behavior.

**Files:**
- Modify: `cmd/vrshare/main.go`

- [ ] **Step 1: Refactor main.go**

Replace the bulk of `main()` with server package calls. Keep the flag parsing and signal handling, but delegate startup to `server.New(cfg).Start(ctx)`. The key change:

```go
func main() {
	// ... existing flag parsing ...
	// ... existing config validation ...

	srv := server.New(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}

	// Display stream info
	state := srv.State()
	fmt.Printf("\nStream URL: %s\n", state.StreamURL)
	copyToClipboard(state.StreamURL)

	// Wait for interrupt
	<-ctx.Done()
	srv.Stop()
}
```

Remove the old inline orchestration code (FFmpeg finding, encoder probing, HLS server start, janitor start, audio start, tunnel start, FFmpeg run) — all of this now lives in `server.Start()`.

Keep `copyToClipboard()` and `getOutboundIP()` can be removed (now in server package).

- [ ] **Step 2: Verify CLI still works**

Run: `go build ./cmd/vrshare/ && ./vrshare.exe --help`
Expected: Builds and shows flags.

- [ ] **Step 3: Commit**

```bash
git add cmd/vrshare/main.go
git commit -m "refactor: use server orchestrator in main.go"
```

---

## Phase 2: Wails Integration

### Task 8: Initialize Wails Project

Set up the Wails project structure, frontend scaffolding with Svelte + Tailwind.

**Files:**
- Create: `wails.json`
- Create: `frontend/package.json`
- Create: `frontend/vite.config.js`
- Create: `frontend/tailwind.config.js`
- Create: `frontend/postcss.config.js`
- Create: `frontend/index.html`
- Create: `frontend/src/main.js`
- Create: `frontend/src/App.svelte`
- Modify: `go.mod` (add Wails dependency)
- Modify: `.gitignore`

- [ ] **Step 1: Install Wails CLI**

Run: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
Expected: Installs successfully.

- [ ] **Step 2: Add Wails dependency to go.mod**

Run: `go get github.com/wailsapp/wails/v2@latest`
Expected: `go.mod` and `go.sum` updated.

- [ ] **Step 3: Create wails.json**

Create `wails.json` in the project root:

```json
{
  "$schema": "https://wails.io/schemas/config.v2.json",
  "name": "vrshare",
  "outputfilename": "vrshare",
  "frontend:install": "npm install",
  "frontend:build": "npm run build",
  "frontend:dev:watcher": "npm run dev",
  "frontend:dev:serverUrl": "auto",
  "author": {
    "name": "VexedAA"
  }
}
```

- [ ] **Step 4: Create frontend scaffolding**

Create `frontend/package.json`:

```json
{
  "name": "vrshare-frontend",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "svelte": "^4.0.0"
  },
  "devDependencies": {
    "@sveltejs/vite-plugin-svelte": "^3.0.0",
    "autoprefixer": "^10.4.0",
    "postcss": "^8.4.0",
    "tailwindcss": "^3.4.0",
    "vite": "^5.0.0"
  }
}
```

Create `frontend/vite.config.js`:

```js
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: 'dist',
  },
});
```

Create `frontend/tailwind.config.js`:

```js
/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{svelte,js,ts}'],
  theme: {
    extend: {},
  },
  plugins: [],
};
```

Create `frontend/postcss.config.js`:

```js
export default {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
  },
};
```

Create `frontend/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>VRShare</title>
</head>
<body class="bg-slate-900 text-slate-200">
  <div id="app"></div>
  <script type="module" src="/src/main.js"></script>
</body>
</html>
```

Create `frontend/src/main.js`:

```js
import './app.css';
import App from './App.svelte';

const app = new App({
  target: document.getElementById('app'),
});

export default app;
```

Create `frontend/src/app.css`:

```css
@tailwind base;
@tailwind components;
@tailwind utilities;
```

Create `frontend/src/App.svelte`:

```svelte
<script>
  let view = 'dashboard'; // 'wizard', 'dashboard', 'settings'
</script>

<main class="min-h-screen bg-slate-900">
  <div class="flex items-center justify-center min-h-screen">
    <p class="text-slate-400 text-lg">VRShare GUI — Loading...</p>
  </div>
</main>
```

- [ ] **Step 5: Install frontend dependencies**

Run: `cd frontend && npm install`
Expected: `node_modules/` created.

- [ ] **Step 6: Update .gitignore**

Add to `.gitignore`:

```
frontend/node_modules/
frontend/dist/
```

- [ ] **Step 7: Verify frontend builds**

Run: `cd frontend && npm run build`
Expected: `frontend/dist/` created with built assets.

- [ ] **Step 8: Commit**

```bash
git add wails.json frontend/ go.mod go.sum .gitignore
git commit -m "feat: initialize Wails project with Svelte + Tailwind"
```

---

### Task 9: GUI App Package

Create `internal/gui/app.go` — the Wails application struct with bindings.

**Files:**
- Create: `internal/gui/app.go`

- [ ] **Step 1: Implement App struct with Wails bindings**

Create `internal/gui/app.go`:

```go
package gui

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/vexedaa/vrshare/internal/config"
	"github.com/vexedaa/vrshare/internal/server"
)

type App struct {
	ctx     context.Context
	srv     *server.Server
	dataDir string
	ticker  *time.Ticker
	done    chan struct{}
}

func NewApp() *App {
	return &App{}
}

// startup is called when the Wails app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	dir, err := server.DataDir()
	if err != nil {
		runtime.LogFatalf(ctx, "Failed to get data dir: %v", err)
	}
	a.dataDir = dir

	cfg, err := server.LoadConfig(dir)
	if err != nil {
		runtime.LogWarningf(ctx, "Failed to load config, using defaults: %v", err)
		cfg = config.Default()
	}
	a.srv = server.New(cfg)
}

// shutdown is called when the Wails app is closing.
func (a *App) shutdown(ctx context.Context) {
	a.stopStatsTicker()
	a.srv.Stop()
}

// StartStream starts the streaming server.
func (a *App) StartStream() error {
	if err := a.srv.Start(a.ctx); err != nil {
		return err
	}
	a.startStatsTicker()
	return nil
}

// StopStream stops the streaming server.
func (a *App) StopStream() error {
	a.stopStatsTicker()
	return a.srv.Stop()
}

// GetState returns the current stream state.
func (a *App) GetState() server.StreamState {
	return a.srv.State()
}

// GetConfig returns the current configuration.
func (a *App) GetConfig() config.Config {
	return a.srv.Config()
}

// SaveConfig saves and applies a new configuration.
func (a *App) SaveConfig(cfg config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	a.srv.SetConfig(cfg)
	return server.SaveConfig(a.dataDir, cfg)
}

// ListPresets returns all saved presets.
func (a *App) ListPresets() ([]server.Preset, error) {
	return server.ListPresets(a.dataDir)
}

// SavePreset saves a named preset.
func (a *App) SavePreset(name string, cfg config.Config) error {
	return server.SavePreset(a.dataDir, name, cfg)
}

// LoadPreset loads a preset by name and applies it.
func (a *App) LoadPreset(name string) (config.Config, error) {
	cfg, err := server.LoadPreset(a.dataDir, name)
	if err != nil {
		return cfg, err
	}
	a.srv.SetConfig(cfg)
	server.SaveConfig(a.dataDir, cfg)
	return cfg, nil
}

// DeletePreset removes a preset by name.
func (a *App) DeletePreset(name string) error {
	return server.DeletePreset(a.dataDir, name)
}

// GetSettings returns app settings.
func (a *App) GetSettings() (server.AppSettings, error) {
	return server.LoadSettings(a.dataDir)
}

// SaveSettings saves app settings.
func (a *App) SaveSettings(s server.AppSettings) error {
	return server.SaveSettings(a.dataDir, s)
}

// GetLogEntries returns recent log entries.
func (a *App) GetLogEntries() []server.LogEntry {
	return a.srv.LogEntries()
}

// startStatsTicker emits stream:state events every second.
func (a *App) startStatsTicker() {
	a.ticker = time.NewTicker(1 * time.Second)
	a.done = make(chan struct{})
	go func() {
		for {
			select {
			case <-a.ticker.C:
				state := a.srv.State()
				runtime.EventsEmit(a.ctx, "stream:state", state)
				entries := a.srv.LogEntries()
				runtime.EventsEmit(a.ctx, "stream:log", entries)
			case <-a.done:
				return
			}
		}
	}()
}

func (a *App) stopStatsTicker() {
	if a.ticker != nil {
		a.ticker.Stop()
	}
	if a.done != nil {
		close(a.done)
		a.done = nil
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/gui/`
Expected: Compiles (may need `go mod tidy` first).

- [ ] **Step 3: Commit**

```bash
git add internal/gui/app.go
git commit -m "feat: add Wails GUI app with bindings"
```

---

### Task 10: System Detection

Create `internal/gui/detect.go` — probes available encoders, monitors, and audio devices.

**Files:**
- Create: `internal/gui/detect.go`
- Create: `internal/gui/detect_windows.go`
- Create: `internal/gui/detect_other.go`

- [ ] **Step 1: Implement DetectSystem**

Create `internal/gui/detect.go`:

```go
package gui

import (
	"github.com/vexedaa/vrshare/internal/ffmpeg"
	"github.com/vexedaa/vrshare/internal/server"
)

// DetectSystem probes the system for available encoders, monitors, and audio devices.
func (a *App) DetectSystem() server.SystemInfo {
	info := server.SystemInfo{}

	// Detect encoders via FFmpeg
	ffmpegPath, err := ffmpeg.FindFFmpeg()
	if err == nil {
		probe := ffmpeg.ProbeFFmpegEncoder(ffmpegPath)
		encoders := []struct {
			name  string
			typ   string
			label string
			enc   string
		}{
			{"h264_nvenc", "nvenc", "NVIDIA NVENC", "h264_nvenc"},
			{"h264_qsv", "qsv", "Intel Quick Sync", "h264_qsv"},
			{"h264_amf", "amf", "AMD AMF", "h264_amf"},
			{"libx264", "cpu", "CPU (libx264)", "libx264"},
		}
		for _, e := range encoders {
			info.Encoders = append(info.Encoders, server.EncoderInfo{
				Name:      e.name,
				Type:      e.typ,
				Label:     e.label,
				Available: probe(e.enc),
			})
		}
	}

	// Platform-specific: monitors and audio devices
	info.Monitors, info.AudioDevices = detectPlatformDevices()

	return info
}
```

Create `internal/gui/detect_windows.go`:

```go
//go:build windows

package gui

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/vexedaa/vrshare/internal/server"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitorsW")
	procGetMonitorInfo      = user32.NewProc("GetMonitorInfoW")
)

type monitorInfoEx struct {
	cbSize    uint32
	rcMonitor struct{ left, top, right, bottom int32 }
	rcWork    struct{ left, top, right, bottom int32 }
	dwFlags   uint32
	szDevice  [32]uint16
}

func detectPlatformDevices() ([]server.MonitorInfo, []server.AudioDevice) {
	monitors := detectMonitors()
	// Audio device enumeration is complex via COM/MMDevice API.
	// For now, return a default device entry. Full enumeration can be added later.
	audioDevices := []server.AudioDevice{
		{Name: "Default Output Device", IsDefault: true},
	}
	return monitors, audioDevices
}

func detectMonitors() []server.MonitorInfo {
	var monitors []server.MonitorInfo
	idx := 0

	callback := syscall.NewCallback(func(hMonitor uintptr, hdc uintptr, lprcClip uintptr, dwData uintptr) uintptr {
		var mi monitorInfoEx
		mi.cbSize = uint32(unsafe.Sizeof(mi))
		procGetMonitorInfo.Call(hMonitor, uintptr(unsafe.Pointer(&mi)))

		w := mi.rcMonitor.right - mi.rcMonitor.left
		h := mi.rcMonitor.bottom - mi.rcMonitor.top
		isPrimary := mi.dwFlags&1 != 0 // MONITORINFOF_PRIMARY

		monitors = append(monitors, server.MonitorInfo{
			Index:      idx,
			Name:       fmt.Sprintf("Monitor %d", idx),
			Resolution: fmt.Sprintf("%dx%d", w, h),
			IsPrimary:  isPrimary,
		})
		idx++
		return 1 // continue enumeration
	})

	procEnumDisplayMonitors.Call(0, 0, callback, 0)
	return monitors
}
```

Create `internal/gui/detect_other.go`:

```go
//go:build !windows

package gui

import "github.com/vexedaa/vrshare/internal/server"

func detectPlatformDevices() ([]server.MonitorInfo, []server.AudioDevice) {
	return []server.MonitorInfo{
		{Index: 0, Name: "Primary Display", Resolution: "unknown", IsPrimary: true},
	}, []server.AudioDevice{
		{Name: "Default Output Device", IsDefault: true},
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/gui/`
Expected: Compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/gui/detect.go internal/gui/detect_windows.go internal/gui/detect_other.go
git commit -m "feat: add system detection for encoders, monitors, audio devices"
```

---

### Task 11: System Tray

Create `internal/gui/tray.go` — system tray icon with status-aware menu.

**Files:**
- Create: `internal/gui/tray.go`

- [ ] **Step 1: Implement system tray**

Create `internal/gui/tray.go`:

```go
package gui

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// setupTray configures the system tray menu.
// Called from the Wails OnStartup hook.
func (a *App) setupTray() {
	// Wails v2 system tray is configured via app options.
	// The tray menu is updated dynamically based on stream state.
	a.updateTrayMenu()
}

// updateTrayMenu rebuilds the tray menu based on current state.
func (a *App) updateTrayMenu() {
	state := a.srv.State()

	if state.Status == "streaming" {
		runtime.WindowSetTitle(a.ctx, "VRShare - Streaming")
	} else {
		runtime.WindowSetTitle(a.ctx, "VRShare")
	}
}

// onTrayShow brings the window to front.
func (a *App) onTrayShow() {
	runtime.WindowShow(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
}
```

Note: Full system tray implementation depends on the Wails v2 systray API or a third-party package like `github.com/energye/systray`. The exact implementation will be refined during development based on available APIs. The tray integration hooks into the Wails app lifecycle.

- [ ] **Step 2: Commit**

```bash
git add internal/gui/tray.go
git commit -m "feat: add system tray scaffold"
```

---

### Task 12: Mode Detection and Main Entry Point

Add Windows console detection and wire up CLI vs GUI mode in `main.go`.

**Files:**
- Create: `cmd/vrshare/mode_windows.go`
- Create: `cmd/vrshare/mode_other.go`
- Modify: `cmd/vrshare/main.go`

- [ ] **Step 1: Create Windows mode detection**

Create `cmd/vrshare/mode_windows.go`:

```go
//go:build windows

package main

import (
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/vexedaa/vrshare/frontend"
	"github.com/vexedaa/vrshare/internal/gui"
)

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
)

func hasConsole() bool {
	hwnd, _, _ := procGetConsoleWindow.Call()
	return hwnd != 0
}

func launchGUI() {
	app := gui.NewApp()

	err := wails.Run(&options.App{
		Title:  "VRShare",
		Width:  900,
		Height: 600,
		AssetServer: &assetserver.Options{
			Assets: frontend.Assets,
		},
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

Create `cmd/vrshare/mode_other.go`:

```go
//go:build !windows

package main

func hasConsole() bool {
	return true
}

func launchGUI() {
	// GUI not supported on non-Windows
	log.Fatal("GUI mode is only supported on Windows")
}
```

- [ ] **Step 2: Create frontend embed file**

Create `frontend/embed.go` in the `frontend/` directory (at project root level):

```go
package frontend

import "embed"

//go:embed all:dist
var Assets embed.FS
```

Note: This file must be at the top-level `frontend/` package, not inside `frontend/src/`.

- [ ] **Step 3: Update main.go with mode detection**

In `cmd/vrshare/main.go`, add mode detection at the top of `main()`:

```go
func main() {
	// Mode detection: no args + no console = GUI mode
	if len(os.Args) == 1 && !hasConsole() {
		launchGUI()
		return
	}

	// CLI mode: existing behavior
	// ... rest of existing main() ...
}
```

- [ ] **Step 4: Verify CLI mode still works**

Run: `go build ./cmd/vrshare/`
Expected: Builds successfully. Running with `--help` shows CLI flags.

- [ ] **Step 5: Commit**

```bash
git add cmd/vrshare/mode_windows.go cmd/vrshare/mode_other.go cmd/vrshare/main.go frontend/embed.go
git commit -m "feat: add mode detection — CLI args or GUI based on console"
```

---

## Phase 3: Frontend (Svelte)

### Task 13: Svelte App Shell and Routing

Set up the root App component with view routing between wizard, dashboard, and settings.

**Files:**
- Modify: `frontend/src/App.svelte`
- Create: `frontend/src/lib/Wizard.svelte` (placeholder)
- Create: `frontend/src/lib/Dashboard.svelte` (placeholder)
- Create: `frontend/src/lib/Settings.svelte` (placeholder)

- [ ] **Step 1: Implement App.svelte with routing**

```svelte
<script>
  import { onMount } from 'svelte';
  import Wizard from './lib/Wizard.svelte';
  import Dashboard from './lib/Dashboard.svelte';
  import Settings from './lib/Settings.svelte';
  import { GetSettings } from '../wailsjs/go/gui/App';

  let view = 'loading';

  onMount(async () => {
    try {
      const settings = await GetSettings();
      view = settings.firstRunComplete ? 'dashboard' : 'wizard';
    } catch {
      view = 'wizard';
    }
  });

  function onWizardComplete() {
    view = 'dashboard';
  }

  function onOpenSettings() {
    view = 'settings';
  }

  function onCloseSettings() {
    view = 'dashboard';
  }
</script>

<main class="min-h-screen bg-slate-900 text-slate-200">
  {#if view === 'loading'}
    <div class="flex items-center justify-center min-h-screen">
      <p class="text-slate-400 text-lg">Loading...</p>
    </div>
  {:else if view === 'wizard'}
    <Wizard on:complete={onWizardComplete} />
  {:else if view === 'dashboard'}
    <Dashboard on:openSettings={onOpenSettings} />
  {:else if view === 'settings'}
    <Settings on:close={onCloseSettings} />
  {/if}
</main>
```

- [ ] **Step 2: Create placeholder components**

Create `frontend/src/lib/Wizard.svelte`:
```svelte
<script>
  import { createEventDispatcher } from 'svelte';
  const dispatch = createEventDispatcher();
</script>

<div class="p-8 text-center">
  <h1 class="text-2xl font-bold mb-4">Welcome to VRShare</h1>
  <p class="text-slate-400">Setup wizard — coming soon</p>
</div>
```

Create `frontend/src/lib/Dashboard.svelte`:
```svelte
<script>
  import { createEventDispatcher } from 'svelte';
  const dispatch = createEventDispatcher();
</script>

<div class="p-8 text-center">
  <h1 class="text-2xl font-bold mb-4">Dashboard</h1>
  <p class="text-slate-400">Dashboard — coming soon</p>
</div>
```

Create `frontend/src/lib/Settings.svelte`:
```svelte
<script>
  import { createEventDispatcher } from 'svelte';
  const dispatch = createEventDispatcher();
</script>

<div class="p-8 text-center">
  <h1 class="text-2xl font-bold mb-4">Settings</h1>
  <p class="text-slate-400">Settings — coming soon</p>
</div>
```

- [ ] **Step 3: Build and verify**

Run: `cd frontend && npm run build`
Expected: Builds successfully.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/
git commit -m "feat: add Svelte app shell with view routing"
```

---

### Task 14: First-Run Wizard

Implement the wizard view — auto-detect system, show results, allow adjustment, save config.

**Files:**
- Modify: `frontend/src/lib/Wizard.svelte`

- [ ] **Step 1: Implement Wizard component**

```svelte
<script>
  import { onMount, createEventDispatcher } from 'svelte';
  import { DetectSystem, SaveConfig, SaveSettings, SavePreset } from '../../wailsjs/go/gui/App';

  const dispatch = createEventDispatcher();

  let systemInfo = null;
  let loading = true;

  // Config values
  let encoder = 'auto';
  let monitor = 0;
  let audioEnabled = true;
  let audioDevice = '';
  let resolution = '1920x1080';
  let fps = 60;
  let bitrate = 4000;
  let port = 8080;

  onMount(async () => {
    try {
      systemInfo = await DetectSystem();

      // Set defaults from detection
      const bestEncoder = systemInfo.encoders?.find(e => e.available);
      if (bestEncoder) encoder = bestEncoder.type;

      const primaryMonitor = systemInfo.monitors?.find(m => m.isPrimary);
      if (primaryMonitor) monitor = primaryMonitor.index;

      const defaultAudio = systemInfo.audioDevices?.find(d => d.isDefault);
      if (defaultAudio) audioDevice = defaultAudio.name;
    } catch (err) {
      console.error('Detection failed:', err);
    }
    loading = false;
  });

  async function save() {
    const cfg = {
      port,
      monitor,
      fps,
      resolution,
      bitrate,
      encoder,
      audio: audioEnabled,
      audioDevice,
      tunnel: '',
    };

    try {
      await SaveConfig(cfg);
      await SavePreset('Default', cfg);
      await SaveSettings({ firstRunComplete: true, closeBehavior: 'tray' });
      dispatch('complete');
    } catch (err) {
      console.error('Save failed:', err);
    }
  }

  const resolutionOptions = ['1920x1080', '2560x1440', '1280x720'];
  const fpsOptions = [30, 60, 120];
</script>

{#if loading}
  <div class="flex items-center justify-center min-h-screen">
    <p class="text-slate-400">Detecting system...</p>
  </div>
{:else}
  <div class="max-w-2xl mx-auto p-8">
    <div class="text-center mb-8">
      <h1 class="text-2xl font-semibold">Welcome to VRShare</h1>
      <p class="text-slate-400 mt-1">We detected your system configuration. Confirm or adjust below.</p>
    </div>

    <div class="grid grid-cols-2 gap-4">
      <!-- Encoder -->
      <div class="bg-slate-800 rounded-lg p-4">
        <div class="text-xs uppercase tracking-wide text-slate-500">Encoder</div>
        <select bind:value={encoder} class="mt-2 w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1">
          {#each (systemInfo?.encoders || []) as enc}
            <option value={enc.type} disabled={!enc.available}>
              {enc.label} {enc.available ? '' : '(unavailable)'}
            </option>
          {/each}
        </select>
      </div>

      <!-- Monitor -->
      <div class="bg-slate-800 rounded-lg p-4">
        <div class="text-xs uppercase tracking-wide text-slate-500">Monitor</div>
        <select bind:value={monitor} class="mt-2 w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1">
          {#each (systemInfo?.monitors || []) as mon}
            <option value={mon.index}>
              {mon.name} — {mon.resolution} {mon.isPrimary ? '(Primary)' : ''}
            </option>
          {/each}
        </select>
      </div>

      <!-- Audio -->
      <div class="bg-slate-800 rounded-lg p-4">
        <div class="text-xs uppercase tracking-wide text-slate-500">Audio</div>
        <div class="flex items-center justify-between mt-2">
          <span class="text-sm text-slate-300">System Audio</span>
          <label class="relative inline-flex items-center cursor-pointer">
            <input type="checkbox" bind:checked={audioEnabled} class="sr-only peer" />
            <div class="w-10 h-5 bg-slate-600 peer-checked:bg-green-500 rounded-full transition-colors">
              <div class="absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform peer-checked:translate-x-5"></div>
            </div>
          </label>
        </div>
        {#if audioEnabled}
          <select bind:value={audioDevice} class="mt-2 w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1">
            {#each (systemInfo?.audioDevices || []) as dev}
              <option value={dev.name}>{dev.name}</option>
            {/each}
          </select>
        {/if}
      </div>

      <!-- Stream Output -->
      <div class="bg-slate-800 rounded-lg p-4">
        <div class="text-xs uppercase tracking-wide text-slate-500">Stream Output</div>
        <div class="grid grid-cols-2 gap-2 mt-2">
          <div>
            <div class="text-xs text-slate-400 mb-1">Resolution</div>
            <select bind:value={resolution} class="w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1">
              {#each resolutionOptions as res}
                <option value={res}>{res}</option>
              {/each}
            </select>
          </div>
          <div>
            <div class="text-xs text-slate-400 mb-1">FPS</div>
            <select bind:value={fps} class="w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1">
              {#each fpsOptions as f}
                <option value={f}>{f}</option>
              {/each}
            </select>
          </div>
          <div>
            <div class="text-xs text-slate-400 mb-1">Bitrate (kbps)</div>
            <input type="number" bind:value={bitrate} class="w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1" />
          </div>
          <div>
            <div class="text-xs text-slate-400 mb-1">Port</div>
            <input type="number" bind:value={port} class="w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1" />
          </div>
        </div>
      </div>
    </div>

    <div class="text-center mt-8">
      <button on:click={save} class="bg-blue-600 hover:bg-blue-500 text-white font-semibold px-8 py-2 rounded-lg transition-colors">
        Save & Continue
      </button>
    </div>
  </div>
{/if}
```

- [ ] **Step 2: Build and verify**

Run: `cd frontend && npm run build`
Expected: Builds successfully.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/Wizard.svelte
git commit -m "feat: implement first-run wizard with system detection"
```

---

### Task 15: Dashboard Component

Implement the dashboard with idle and streaming states, stats row, and event log.

**Files:**
- Modify: `frontend/src/lib/Dashboard.svelte`
- Create: `frontend/src/lib/StatsRow.svelte`
- Create: `frontend/src/lib/EventLog.svelte`
- Create: `frontend/src/lib/PresetPicker.svelte`

- [ ] **Step 1: Create StatsRow component**

Create `frontend/src/lib/StatsRow.svelte`:

```svelte
<script>
  export let state = {};

  function formatBitrate(kbps) {
    return kbps ? kbps.toLocaleString() : '0';
  }
</script>

<div class="grid grid-cols-5 gap-px bg-slate-800">
  <div class="bg-slate-900 p-4 text-center">
    <div class="text-xs uppercase tracking-wide text-slate-500">FPS</div>
    <div class="text-2xl font-bold" class:text-green-400={state.fps >= 58} class:text-yellow-400={state.fps > 0 && state.fps < 58} class:text-slate-200={!state.fps}>
      {state.fps?.toFixed(1) || '0.0'}
    </div>
  </div>
  <div class="bg-slate-900 p-4 text-center">
    <div class="text-xs uppercase tracking-wide text-slate-500">Bitrate</div>
    <div class="text-2xl font-bold text-slate-200">
      {formatBitrate(state.bitrate)} <span class="text-sm text-slate-400">kbps</span>
    </div>
  </div>
  <div class="bg-slate-900 p-4 text-center">
    <div class="text-xs uppercase tracking-wide text-slate-500">Dropped</div>
    <div class="text-2xl font-bold" class:text-slate-200={!state.droppedFrames} class:text-red-400={state.droppedFrames > 0}>
      {state.droppedFrames || 0}
    </div>
  </div>
  <div class="bg-slate-900 p-4 text-center">
    <div class="text-xs uppercase tracking-wide text-slate-500">Speed</div>
    <div class="text-2xl font-bold" class:text-green-400={state.speed >= 0.98} class:text-yellow-400={state.speed > 0 && state.speed < 0.98}>
      {state.speed?.toFixed(2) || '0.00'}<span class="text-sm text-slate-400">x</span>
    </div>
  </div>
  <div class="bg-slate-900 p-4 text-center">
    <div class="text-xs uppercase tracking-wide text-slate-500">Viewers</div>
    <div class="text-2xl font-bold text-slate-200">{state.viewerCount || 0}</div>
  </div>
</div>
```

- [ ] **Step 2: Create EventLog component**

Create `frontend/src/lib/EventLog.svelte`:

```svelte
<script>
  export let entries = [];

  function formatTime(timeStr) {
    if (!timeStr) return '';
    const d = new Date(timeStr);
    return d.toLocaleTimeString('en-US', { hour12: false });
  }
</script>

<div class="p-4">
  <div class="text-xs uppercase tracking-wide text-slate-500 mb-3">Event Log</div>
  <div class="font-mono text-sm text-slate-400 space-y-1 max-h-48 overflow-y-auto">
    {#each [...entries].reverse() as entry}
      <div>
        <span class="text-slate-600">{formatTime(entry.time)}</span>
        {entry.message}
      </div>
    {:else}
      <div class="text-slate-600">No events</div>
    {/each}
  </div>
</div>
```

- [ ] **Step 3: Create PresetPicker component**

Create `frontend/src/lib/PresetPicker.svelte`:

```svelte
<script>
  import { onMount, createEventDispatcher } from 'svelte';
  import { ListPresets, LoadPreset } from '../../wailsjs/go/gui/App';

  const dispatch = createEventDispatcher();

  export let disabled = false;
  let presets = [];
  let selectedName = 'Default';

  onMount(loadPresets);

  async function loadPresets() {
    try {
      presets = await ListPresets() || [];
      if (presets.length > 0 && !presets.find(p => p.name === selectedName)) {
        selectedName = presets[0].name;
      }
    } catch (err) {
      console.error('Failed to load presets:', err);
    }
  }

  async function onSelect(event) {
    selectedName = event.target.value;
    try {
      const cfg = await LoadPreset(selectedName);
      dispatch('loaded', { name: selectedName, config: cfg });
    } catch (err) {
      console.error('Failed to load preset:', err);
    }
  }
</script>

<select value={selectedName} on:change={onSelect} {disabled}
  class="bg-slate-800 text-slate-200 border border-slate-700 rounded-md px-3 py-1.5 text-sm">
  {#each presets as preset}
    <option value={preset.name}>{preset.name}</option>
  {/each}
</select>
```

- [ ] **Step 4: Implement Dashboard component**

```svelte
<script>
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { StartStream, StopStream, GetState, GetConfig, GetLogEntries } from '../../wailsjs/go/gui/App';
  import { EventsOn } from '../../wailsjs/runtime/runtime';
  import StatsRow from './StatsRow.svelte';
  import EventLog from './EventLog.svelte';
  import PresetPicker from './PresetPicker.svelte';

  const dispatch = createEventDispatcher();

  let state = { status: 'idle' };
  let config = {};
  let logEntries = [];
  let copied = false;
  let unsubState;
  let unsubLog;

  onMount(async () => {
    state = await GetState();
    config = await GetConfig();
    logEntries = await GetLogEntries();

    unsubState = EventsOn('stream:state', (s) => { state = s; });
    unsubLog = EventsOn('stream:log', (entries) => { logEntries = entries; });
  });

  onDestroy(() => {
    unsubState?.();
    unsubLog?.();
  });

  async function start() {
    try {
      await StartStream();
      state = await GetState();
    } catch (err) {
      console.error('Start failed:', err);
    }
  }

  async function stop() {
    try {
      await StopStream();
      state = await GetState();
    } catch (err) {
      console.error('Stop failed:', err);
    }
  }

  function copyURL() {
    navigator.clipboard.writeText(state.streamURL);
    copied = true;
    setTimeout(() => copied = false, 2000);
  }

  function formatUptime(ns) {
    if (!ns) return '00:00:00';
    const totalSec = Math.floor(ns / 1e9);
    const h = Math.floor(totalSec / 3600);
    const m = Math.floor((totalSec % 3600) / 60);
    const s = totalSec % 60;
    return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
  }

  $: streaming = state.status === 'streaming';
</script>

<!-- Top Bar -->
<div class="flex justify-between items-center px-6 py-4 border-b border-slate-800">
  <div class="flex items-center gap-3">
    <div class="w-2.5 h-2.5 rounded-full"
      class:bg-green-500={streaming}
      class:shadow-green={streaming}
      class:bg-slate-500={!streaming}></div>
    <span class="font-semibold">{streaming ? 'Streaming' : 'Idle'}</span>
    {#if streaming}
      <span class="text-slate-500">|</span>
      <span class="text-slate-400">{formatUptime(state.uptime)}</span>
    {/if}
  </div>
  <div class="flex items-center gap-4">
    {#if streaming}
      <div class="bg-slate-800 rounded-md px-3 py-1.5 flex items-center gap-2">
        <code class="text-sky-400 text-sm">{state.streamURL}</code>
        <button on:click={copyURL} class="text-slate-500 hover:text-slate-300 text-xs">
          {copied ? 'Copied!' : '[copy]'}
        </button>
      </div>
      <button on:click={stop} class="bg-red-600 hover:bg-red-500 text-white font-semibold px-4 py-1.5 rounded-md transition-colors">
        Stop
      </button>
    {:else}
      <PresetPicker disabled={streaming} />
      <button on:click={start} class="bg-green-600 hover:bg-green-500 text-slate-900 font-semibold px-4 py-1.5 rounded-md transition-colors">
        Start Stream
      </button>
    {/if}
  </div>
</div>

{#if streaming}
  <!-- Stats Row -->
  <StatsRow {state} />

  <!-- Bottom Split -->
  <div class="grid grid-cols-[250px_1fr] min-h-[250px]">
    <!-- Active Preset -->
    <div class="p-4 border-r border-slate-800">
      <div class="text-xs uppercase tracking-wide text-slate-500 mb-3">Active Preset</div>
      <div class="bg-slate-800 rounded-md p-3">
        <div class="font-semibold">Current</div>
        <div class="text-sm text-slate-400 mt-1">
          {config.resolution || 'Native'} @ {config.fps}fps
        </div>
        <div class="text-sm text-slate-400">
          {config.bitrate}kbps · Port {config.port}
        </div>
        <div class="text-sm text-slate-400">
          Audio: {config.audio ? 'On' : 'Off'}
        </div>
      </div>
      <button on:click={() => dispatch('openSettings')} class="text-sky-400 hover:text-sky-300 text-sm mt-4">
        Settings
      </button>
    </div>

    <!-- Event Log -->
    <EventLog entries={logEntries} />
  </div>
{:else}
  <!-- Idle State -->
  <div class="flex items-center justify-center min-h-[350px] text-slate-600">
    <div class="text-center">
      <div class="text-3xl mb-2">Ready to stream</div>
      <div class="text-sm">Select a preset and click Start Stream</div>
      <button on:click={() => dispatch('openSettings')} class="text-sky-400 hover:text-sky-300 text-sm mt-4">
        Settings
      </button>
    </div>
  </div>
{/if}

<style>
  .shadow-green {
    box-shadow: 0 0 6px theme('colors.green.500');
  }
</style>
```

- [ ] **Step 5: Build and verify**

Run: `cd frontend && npm run build`
Expected: Builds successfully.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/
git commit -m "feat: implement dashboard with stats, log, and preset picker"
```

---

### Task 16: Settings View

Implement the full settings view with video, audio, network, app, and presets sections.

**Files:**
- Modify: `frontend/src/lib/Settings.svelte`

- [ ] **Step 1: Implement Settings component**

```svelte
<script>
  import { onMount, createEventDispatcher } from 'svelte';
  import { GetConfig, SaveConfig, GetSettings, SaveSettings, DetectSystem,
           ListPresets, SavePreset, DeletePreset } from '../../wailsjs/go/gui/App';

  const dispatch = createEventDispatcher();

  let config = {};
  let settings = {};
  let systemInfo = null;
  let presets = [];
  let newPresetName = '';
  let error = '';
  let saved = false;

  onMount(async () => {
    config = await GetConfig();
    settings = await GetSettings();
    systemInfo = await DetectSystem();
    presets = await ListPresets() || [];
  });

  async function save() {
    error = '';
    try {
      await SaveConfig(config);
      await SaveSettings(settings);
      saved = true;
      setTimeout(() => saved = false, 2000);
    } catch (err) {
      error = err.toString();
    }
  }

  function cancel() {
    dispatch('close');
  }

  async function saveNewPreset() {
    if (!newPresetName.trim()) return;
    try {
      await SavePreset(newPresetName.trim(), config);
      presets = await ListPresets() || [];
      newPresetName = '';
    } catch (err) {
      error = err.toString();
    }
  }

  async function removePreset(name) {
    try {
      await DeletePreset(name);
      presets = await ListPresets() || [];
    } catch (err) {
      error = err.toString();
    }
  }

  const resolutionOptions = ['1920x1080', '2560x1440', '1280x720'];
  const fpsOptions = [30, 60, 120];
</script>

<div class="max-w-2xl mx-auto p-8">
  <div class="flex justify-between items-center mb-6">
    <h1 class="text-2xl font-semibold">Settings</h1>
    <button on:click={cancel} class="text-slate-400 hover:text-slate-200 text-sm">Back to Dashboard</button>
  </div>

  {#if error}
    <div class="bg-red-900/50 border border-red-700 rounded-md p-3 mb-4 text-red-300 text-sm">{error}</div>
  {/if}

  <!-- Video -->
  <section class="mb-6">
    <h2 class="text-lg font-semibold mb-3 text-slate-300">Video</h2>
    <div class="grid grid-cols-2 gap-4">
      <div>
        <label class="text-xs text-slate-400 block mb-1">Encoder</label>
        <select bind:value={config.encoder} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          <option value="auto">Auto (best available)</option>
          {#each (systemInfo?.encoders || []) as enc}
            <option value={enc.type} disabled={!enc.available}>{enc.label}</option>
          {/each}
        </select>
      </div>
      <div>
        <label class="text-xs text-slate-400 block mb-1">Monitor</label>
        <select bind:value={config.monitor} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          {#each (systemInfo?.monitors || []) as mon}
            <option value={mon.index}>{mon.name} — {mon.resolution}</option>
          {/each}
        </select>
      </div>
      <div>
        <label class="text-xs text-slate-400 block mb-1">Resolution</label>
        <select bind:value={config.resolution} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          <option value="">Native</option>
          {#each resolutionOptions as res}
            <option value={res}>{res}</option>
          {/each}
        </select>
      </div>
      <div>
        <label class="text-xs text-slate-400 block mb-1">FPS</label>
        <select bind:value={config.fps} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          {#each fpsOptions as f}
            <option value={f}>{f}</option>
          {/each}
        </select>
      </div>
      <div>
        <label class="text-xs text-slate-400 block mb-1">Bitrate (kbps)</label>
        <input type="number" bind:value={config.bitrate} min="100" max="50000"
          class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5" />
      </div>
    </div>
  </section>

  <!-- Audio -->
  <section class="mb-6">
    <h2 class="text-lg font-semibold mb-3 text-slate-300">Audio</h2>
    <div class="flex items-center gap-4 mb-3">
      <label class="text-sm text-slate-300">Enable audio capture</label>
      <input type="checkbox" bind:checked={config.audio} class="rounded" />
    </div>
    {#if config.audio}
      <div>
        <label class="text-xs text-slate-400 block mb-1">Audio Device</label>
        <select bind:value={config.audioDevice} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          {#each (systemInfo?.audioDevices || []) as dev}
            <option value={dev.name}>{dev.name}</option>
          {/each}
        </select>
      </div>
    {/if}
  </section>

  <!-- Network -->
  <section class="mb-6">
    <h2 class="text-lg font-semibold mb-3 text-slate-300">Network</h2>
    <div class="grid grid-cols-2 gap-4">
      <div>
        <label class="text-xs text-slate-400 block mb-1">Port</label>
        <input type="number" bind:value={config.port} min="1" max="65535"
          class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5" />
      </div>
      <div>
        <label class="text-xs text-slate-400 block mb-1">Tunnel Provider</label>
        <select bind:value={config.tunnel} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          <option value="">None</option>
          <option value="cloudflare">Cloudflare</option>
          <option value="tailscale">Tailscale</option>
        </select>
      </div>
    </div>
  </section>

  <!-- App -->
  <section class="mb-6">
    <h2 class="text-lg font-semibold mb-3 text-slate-300">App</h2>
    <div>
      <label class="text-xs text-slate-400 block mb-1">When window is closed</label>
      <select bind:value={settings.closeBehavior} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5 max-w-xs">
        <option value="tray">Minimize to system tray</option>
        <option value="quit">Quit application</option>
      </select>
    </div>
  </section>

  <!-- Presets -->
  <section class="mb-6">
    <h2 class="text-lg font-semibold mb-3 text-slate-300">Presets</h2>
    <div class="flex gap-2 mb-3">
      <input type="text" bind:value={newPresetName} placeholder="New preset name..."
        class="flex-1 bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5" />
      <button on:click={saveNewPreset} class="bg-blue-600 hover:bg-blue-500 text-white px-4 py-1.5 rounded text-sm">
        Save Current
      </button>
    </div>
    {#each presets as preset}
      <div class="flex justify-between items-center bg-slate-800 rounded px-3 py-2 mb-1">
        <span class="text-sm">{preset.name}</span>
        <button on:click={() => removePreset(preset.name)} class="text-red-400 hover:text-red-300 text-xs">Delete</button>
      </div>
    {/each}
  </section>

  <!-- Actions -->
  <div class="flex gap-3 pt-4 border-t border-slate-800">
    <button on:click={save} class="bg-blue-600 hover:bg-blue-500 text-white font-semibold px-6 py-2 rounded-md transition-colors">
      {saved ? 'Saved!' : 'Save'}
    </button>
    <button on:click={cancel} class="bg-slate-700 hover:bg-slate-600 text-slate-200 px-6 py-2 rounded-md transition-colors">
      Cancel
    </button>
  </div>
</div>
```

- [ ] **Step 2: Build and verify**

Run: `cd frontend && npm run build`
Expected: Builds successfully.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/Settings.svelte
git commit -m "feat: implement settings view with presets management"
```

---

## Phase 4: Integration & Polish

### Task 17: Window Close Behavior

Wire up the configurable close behavior (minimize to tray vs quit) in the Wails app.

**Files:**
- Modify: `internal/gui/app.go`

- [ ] **Step 1: Add close handler to App**

In `internal/gui/app.go`, add a `beforeClose` handler that checks the close behavior setting:

```go
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	settings, err := server.LoadSettings(a.dataDir)
	if err != nil || settings.CloseBehavior == "quit" {
		a.srv.Stop()
		return false // allow close
	}
	// Minimize to tray
	runtime.WindowHide(ctx)
	return true // prevent close
}
```

This method is registered in the Wails app options via `OnBeforeClose: app.beforeClose`.

- [ ] **Step 2: Update launchGUI to include OnBeforeClose**

In `cmd/vrshare/mode_windows.go`, add `OnBeforeClose` to the Wails options:

```go
OnBeforeClose: app.BeforeClose,
```

And expose it as a public method in `app.go`:

```go
func (a *App) BeforeClose(ctx context.Context) bool {
	return a.beforeClose(ctx)
}

func (a *App) Startup(ctx context.Context) {
	a.startup(ctx)
}

func (a *App) Shutdown(ctx context.Context) {
	a.shutdown(ctx)
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/gui/app.go cmd/vrshare/mode_windows.go
git commit -m "feat: configurable window close behavior (tray vs quit)"
```

---

### Task 18: End-to-End Build Verification

Verify the complete application builds and the Wails dev mode works.

**Files:** None (verification only)

- [ ] **Step 1: Run go mod tidy**

Run: `go mod tidy`
Expected: All dependencies resolved.

- [ ] **Step 2: Build the full application with Wails**

Run: `wails build`
Expected: Produces `build/bin/vrshare.exe` with embedded frontend.

- [ ] **Step 3: Test CLI mode**

Run: `./build/bin/vrshare.exe --port 9090 --fps 30`
Expected: Starts in headless CLI mode, streams on port 9090.

- [ ] **Step 4: Test GUI mode**

Double-click `build/bin/vrshare.exe` from Explorer.
Expected: GUI window opens with wizard (first run) or dashboard.

- [ ] **Step 5: Run all Go tests**

Run: `go test ./... -v`
Expected: All tests pass (existing + new).

- [ ] **Step 6: Commit any fixes**

```bash
git add -A
git commit -m "fix: resolve build issues from end-to-end verification"
```

---

## Task Dependency Graph

```
Task 1 (config extend) ─────┐
Task 2 (stats parser) ──────┤
Task 3 (viewer count) ──────┼──▶ Task 6 (server orchestrator) ──▶ Task 7 (refactor main.go)
Task 4 (persistence) ───────┤
Task 5 (manager hook) ──────┘
                                    │
Task 8 (Wails init) ────────────────┼──▶ Task 9 (gui app) ──▶ Task 10 (detect) ──▶ Task 11 (tray)
                                    │                                                    │
                                    ▼                                                    ▼
                             Task 12 (mode detect) ◀─────────────────────────────────────┘
                                    │
                                    ▼
                             Task 13 (app shell) ──▶ Task 14 (wizard) ──▶ Task 15 (dashboard) ──▶ Task 16 (settings)
                                                                                                        │
                                                                                                        ▼
                                                                                              Task 17 (close behavior)
                                                                                                        │
                                                                                                        ▼
                                                                                              Task 18 (e2e verify)
```

**Parallelizable:** Tasks 1-5 can all run in parallel. Tasks 8 can run in parallel with Tasks 1-5. Task 13 can start once Task 8 completes.
