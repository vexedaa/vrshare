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
	defaults := config.Default()
	cfg := defaults // start with defaults so new fields have sane values
	err := readJSON(filepath.Join(dir, "config.json"), &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
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

// SessionLogEntry describes a past session log file.
type SessionLogEntry struct {
	Name    string `json:"name"`    // filename
	Date    string `json:"date"`    // human-readable date
	Size    int64  `json:"size"`    // bytes
}

// ListSessionLogs returns available session log files, newest first.
func ListSessionLogs(dir string) ([]SessionLogEntry, error) {
	logsDir := filepath.Join(dir, "logs")
	entries, err := os.ReadDir(logsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var logs []SessionLogEntry
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		// Parse date from filename: session-2026-03-28_15-04-05.log
		date := e.Name()
		date = strings.TrimPrefix(date, "session-")
		date = strings.TrimSuffix(date, ".log")
		date = strings.Replace(date, "_", " ", 1)

		logs = append(logs, SessionLogEntry{
			Name: e.Name(),
			Date: date,
			Size: info.Size(),
		})
	}
	return logs, nil
}

// ReadSessionLog returns the contents of a session log file.
func ReadSessionLog(dir, name string) (string, error) {
	// Prevent path traversal
	clean := filepath.Base(name)
	if !strings.HasSuffix(clean, ".log") {
		return "", errors.New("invalid log file")
	}
	data, err := os.ReadFile(filepath.Join(dir, "logs", clean))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func sanitizeName(name string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return r.Replace(name)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
