package gui

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/vexedaa/vrshare/internal/config"
	"github.com/vexedaa/vrshare/internal/ffmpeg"
	"github.com/vexedaa/vrshare/internal/server"
)

// App is the Wails application struct with frontend bindings.
type App struct {
	ctx      context.Context
	srv      *server.Server
	dataDir  string
	ticker   *time.Ticker
	done     chan struct{}
	quitting bool // set when quit is requested from tray
}

// NewApp creates a new App instance and sets up debug logging.
func NewApp() *App {
	// Log to file since GUI mode has no console
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, ".vrshare", "debug.log")
	os.MkdirAll(filepath.Dir(logPath), 0755)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err == nil {
		log.SetOutput(f)
	}
	log.Println("[app] NewApp created, logging to", logPath)
	return &App{}
}

// Startup is called when the Wails app starts.
func (a *App) Startup(ctx context.Context) {
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
	a.setupTray()
}

// Shutdown is called when the Wails app is closing.
func (a *App) Shutdown(_ context.Context) {
	a.removeTray()
	a.stopStatsTicker()
	if a.srv != nil {
		a.srv.Stop()
	}
}

// BeforeClose handles window close based on user settings.
func (a *App) BeforeClose(_ context.Context) bool {
	// If quit was requested from tray, always allow
	if a.quitting {
		return false
	}

	settings, err := server.LoadSettings(a.dataDir)
	if err != nil || settings.CloseBehavior == "quit" {
		a.removeTray()
		if a.srv != nil {
			a.srv.Stop()
		}
		return false // allow close
	}
	// Minimize to taskbar (keeps stream running, click taskbar to restore)
	runtime.WindowMinimise(a.ctx)
	return true // prevent close
}

// StartStream starts the streaming server.
func (a *App) StartStream() error {
	if err := a.srv.Start(a.ctx); err != nil {
		return err
	}
	a.startStatsTicker()
	a.UpdateTrayIcon(true)
	return nil
}

// RestartStream restarts only the capture (FFmpeg) with current config.
// The HLS server, tunnel, and audio stay running.
func (a *App) RestartStream() error {
	return a.srv.RestartCapture()
}

// SwitchMonitor changes the capture monitor and restarts FFmpeg.
func (a *App) SwitchMonitor(index int) error {
	cfg := a.srv.Config()
	cfg.Monitor = index
	a.srv.SetConfig(cfg)
	server.SaveConfig(a.dataDir, cfg)
	state := a.srv.State()
	if state.Status == "streaming" {
		return a.srv.RestartCapture()
	}
	return nil
}

// StopStream stops the streaming server.
func (a *App) StopStream() error {
	a.stopStatsTicker()
	a.UpdateTrayIcon(false)
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

// DownloadFFmpeg downloads and installs FFmpeg automatically.
func (a *App) DownloadFFmpeg() (string, error) {
	return ffmpeg.Download()
}

// HasFFmpeg checks if FFmpeg is available.
func (a *App) HasFFmpeg() bool {
	_, err := ffmpeg.FindFFmpeg()
	return err == nil
}

// TunnelProviderStatus describes the state of a tunnel provider.
type TunnelProviderStatus struct {
	Name       string `json:"name"`       // "cloudflare" or "tailscale"
	Label      string `json:"label"`      // human-readable
	Installed  bool   `json:"installed"`  // binary found on PATH
	Authorized bool   `json:"authorized"` // logged in / ready to use
	StatusText string `json:"statusText"` // e.g. "Ready", "Not installed", "Not logged in"
}

// GetTunnelProviders returns the status of all supported tunnel providers.
func (a *App) GetTunnelProviders() []TunnelProviderStatus {
	return checkTunnelProviders()
}

// AuthorizeTunnel initiates authorization for a tunnel provider.
// Returns a message describing what happened (e.g. "Opening browser for login").
func (a *App) AuthorizeTunnel(provider string) (string, error) {
	return authorizeTunnel(provider)
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
		select {
		case <-a.done:
			// already closed
		default:
			close(a.done)
		}
		a.done = nil
	}
}
