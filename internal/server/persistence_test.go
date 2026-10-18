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

// v1.0.5 users calibrated the audio sync offset against the old timing, where
// every FFmpeg start added a random 250-400ms of audio lag and lost audio
// pushed it the other way. Under the corrected timing those values are wrong,
// so they must not carry over; offsets saved from now on must.
func TestLoadConfigDropsSyncOffsetCalibratedForOldTiming(t *testing.T) {
	dir := t.TempDir()
	v105 := `{"port": 8080, "fps": 60, "bitrate": 4000, "encoder": "nvenc", "audio": true, "audioOffsetMs": -560}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(v105), 0644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.AudioOffsetMs != 0 {
		t.Errorf("AudioOffsetMs = %d, want 0: a calibration for the old timing must be dropped", loaded.AudioOffsetMs)
	}
	if loaded.FPS != 60 {
		t.Errorf("FPS = %d, want 60: the rest of the config must still load", loaded.FPS)
	}

	cfg := config.Default()
	cfg.AudioOffsetMs = 120
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	if loaded, _ := LoadConfig(dir); loaded.AudioOffsetMs != 120 {
		t.Errorf("AudioOffsetMs after save/load = %d, want 120", loaded.AudioOffsetMs)
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
