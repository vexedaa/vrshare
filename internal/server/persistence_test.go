package server

import (
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
	if s.FirstRunComplete || s.CloseBehavior != "tray" {
		t.Errorf("expected default settings when file missing")
	}
}

func TestPresetCRUD(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Port = 7070

	if err := SavePreset(dir, "My Preset", cfg); err != nil {
		t.Fatalf("SavePreset: %v", err)
	}

	presets, err := ListPresets(dir)
	if err != nil {
		t.Fatalf("ListPresets: %v", err)
	}
	if len(presets) != 1 || presets[0].Name != "My Preset" {
		t.Errorf("expected 1 preset named 'My Preset', got %+v", presets)
	}

	loaded, err := LoadPreset(dir, "My Preset")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	if loaded.Port != 7070 {
		t.Errorf("loaded preset port = %d, want 7070", loaded.Port)
	}

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
	if filepath.Base(d) != ".vrshare" {
		t.Errorf("DataDir should end with .vrshare, got %s", d)
	}
}
