# VRShare GUI Design Spec

## Overview

Add a Wails-based GUI to VRShare that supersedes the CLI as the primary user interface. The existing CLI behavior is preserved: when launched with command-line arguments, VRShare behaves exactly as it does today. When launched without arguments (e.g., double-clicked from Explorer), it opens a native GUI window with a system tray icon.

## Architecture

### Approach: Unified Binary with Mode Detection

A single `vrshare` binary detects its launch context:

- **CLI args present** → parse flags, start server headless, block until Ctrl+C (current behavior)
- **No CLI args + no console** (double-clicked) → launch Wails GUI with system tray
- **No CLI args + console** → print help/usage

Console detection on Windows uses `GetConsoleWindow()` from the Windows API. GUI mode is Windows-only (matching existing platform constraints: WASAPI, ddagrab).

### Project Structure

```
cmd/vrshare/
  main.go              — mode detection: CLI args → headless, no args → GUI
internal/
  config/config.go     — (existing, unchanged) config struct + validation
  ffmpeg/              — (existing, unchanged) encoder detection, command building, process manager
  hls/                 — (existing, unchanged) HTTP server, player, janitor
  audio/               — (existing, unchanged) WASAPI capturer
  tunnel/              — (existing, unchanged) Cloudflare/Tailscale
  server/              — (new) orchestrator: wires together ffmpeg, hls, audio, tunnel
  gui/                 — (new) Wails app, system tray, frontend bindings
frontend/              — (new) Svelte + Tailwind CSS web frontend
```

### Design Constraints

- All existing `internal/` packages remain untouched and Wails-free.
- `internal/server/` is the only new package that imports existing internal packages.
- `internal/gui/` is the only package that imports Wails.
- The CLI code path never imports Wails or `internal/gui/`.

## New Package: `internal/server/`

Extracts the orchestration logic currently in `main.go` into a reusable struct.

```go
type Server struct { ... }

// Lifecycle
func New(cfg config.Config) *Server
func (s *Server) Start(ctx context.Context) error
func (s *Server) Stop() error

// State
func (s *Server) State() StreamState
func (s *Server) Config() config.Config
```

### StreamState

```go
type StreamState struct {
    Status        string        // "idle", "starting", "streaming", "error"
    Error         string        // populated when Status == "error"
    Uptime        time.Duration
    StreamURL     string        // built from configured port + LAN IP

    // Encoding stats (populated while streaming)
    FPS           float64
    Bitrate       int           // kbps
    DroppedFrames int
    CPUUsage      float64
    GPUUsage      float64
    ViewerCount   int
}
```

### Behavior

- `Start()` performs what `main.go` does today: probe FFmpeg, detect encoder, create temp dir, start HLS server, start audio capturer (if enabled), start tunnel (if configured), launch FFmpeg process.
- `Stop()` gracefully shuts down all components.
- `State()` returns current stream state. Encoding stats are parsed from FFmpeg's stderr progress output. Viewer count is derived from active HTTP connections to the HLS playlist endpoint.
- Both CLI and GUI call the same `Server` instance.

## New Package: `internal/gui/`

Owns everything Wails-specific.

### Wails Bindings (Go methods exposed to frontend)

```go
// Stream control
func (a *App) StartStream() error
func (a *App) StopStream() error
func (a *App) GetState() server.StreamState
func (a *App) GetConfig() config.Config
func (a *App) SaveConfig(cfg config.Config) error

// Presets
func (a *App) ListPresets() []Preset
func (a *App) SavePreset(name string, cfg config.Config) error
func (a *App) LoadPreset(name string) (config.Config, error)
func (a *App) DeletePreset(name string) error

// System detection (for wizard)
func (a *App) DetectSystem() SystemInfo  // available encoders, monitors, audio devices

// App settings
func (a *App) GetSettings() AppSettings
func (a *App) SaveSettings(s AppSettings) error
```

### Wails Events (backend → frontend)

Emitted on a 1-second tick while streaming:

- `stream:state` — full `StreamState` struct
- `stream:log` — new timestamped log entries

No events emitted when idle.

### System Tray

**Right-click menu:**
- Show VRShare — brings window to front
- Start Stream / Stop Stream — toggles based on current state
- _(separator)_
- Stream URL — shown when streaming, click to copy to clipboard
- _(separator)_
- Quit — stops stream if running, fully exits

**Icon states:**
- Idle: default icon (grey/neutral)
- Streaming: active icon (green tint or overlay)
- Error: error icon (red tint or overlay)

**Double-click tray icon** → show window.

### Window Close Behavior

Configurable via app settings:
- **Minimize to tray** (default) — closing the window hides to tray, stream keeps running
- **Quit** — closing the window stops the stream and exits

## Frontend (Svelte + Tailwind CSS)

### Tech Stack

- **Svelte** — lightweight compiled framework, first-class Wails support
- **Tailwind CSS** — utility-first styling

### Views

#### 1. First-Run Wizard

Shown on first launch (when `settings.json` doesn't exist or `firstRunComplete` is false).

Single confirmation screen with four configurable sections:

- **Encoder** — auto-detected with GPU name shown, dropdown to override (NVENC/QSV/AMF/CPU)
- **Monitor** — auto-detected primary, dropdown to select alternative
- **Audio** — enable/disable toggle + device dropdown
- **Stream Output** — resolution dropdown (1080p/1440p/720p/custom), FPS dropdown (30/60/120), bitrate input (kbps), port input

"Save & Continue" button saves config and marks first run complete.

#### 2. Dashboard

Main view after first run. Two states:

**Idle state:**
- Top bar: grey status dot, "Idle" label, preset selector dropdown, "Start Stream" button
- Center: "Ready to stream" message

**Streaming state:**
- Top bar: green status dot, "Streaming" label, uptime counter, stream URL with copy button, "Stop" button
- Stats row: FPS, bitrate, dropped frames, CPU usage, viewer count
- Bottom split:
  - Left panel: active preset summary (name, key settings)
  - Right panel: scrolling event log (timestamped messages)

#### 3. Settings

Accessible from dashboard via "Settings" link. Organized into sections:

- **Video** — encoder, monitor, resolution, FPS, bitrate
- **Audio** — enable/disable toggle, device dropdown
- **Network** — port, tunnel provider (none/Cloudflare/Tailscale)
- **App** — close behavior (minimize to tray / quit)
- **Presets** — save current config as preset (name + save), list saved presets with load/delete

Save and Cancel buttons. Inline validation errors (port out of range, invalid bitrate, etc.).

## Data Persistence

All user data stored in `~/.vrshare/`:

```
~/.vrshare/
  config.json          — current active configuration
  settings.json        — app preferences (close behavior, firstRunComplete flag)
  presets/
    Default.json       — created on first run (1080p, 60fps, 4000kbps, audio enabled)
    <user presets>.json — user-created presets
  ffmpeg/              — (existing) FFmpeg binary location
```

- Config format matches `config.Config` struct, serialized as JSON.
- Presets are config snapshots with a name. Loading a preset overwrites `config.json`.
- First-run detection: `settings.json` absent or `firstRunComplete: false` → show wizard.

## End-to-End Data Flow

1. User configures port/settings in wizard or settings view
2. Config saved to `~/.vrshare/config.json`
3. User clicks "Start Stream"
4. Frontend calls `App.StartStream()` → creates `server.Server` with config → calls `Start()`
5. Server starts HLS on configured port, builds `StreamURL` from port + LAN IP
6. Backend emits `stream:state` events every second with live stats
7. Frontend reactively updates dashboard with incoming state
8. Stream URL displayed with copy button reflects the actual configured port
9. Tray icon updates to streaming state
10. User clicks "Stop" → `App.StopStream()` → `Server.Stop()` → graceful shutdown
11. Dashboard returns to idle state, tray icon returns to idle

## Default Preset

The "Default" preset created on first run:
- Resolution: 1920x1080
- FPS: 60
- Bitrate: 4000 kbps
- Audio: enabled
- Port: 8080
- Encoder: auto (best available)
- Tunnel: none
