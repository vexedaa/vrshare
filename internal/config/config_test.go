package config

import (
	"testing"
)

func TestDefault_IsValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
}

func TestValidate_PortRange(t *testing.T) {
	tests := []struct {
		port    int
		wantErr bool
	}{
		{0, true},
		{1, false},
		{8080, false},
		{65535, false},
		{65536, true},
		{-1, true},
	}
	for _, tt := range tests {
		cfg := Default()
		cfg.Port = tt.port
		err := cfg.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("port=%d: wantErr=%v, got %v", tt.port, tt.wantErr, err)
		}
	}
}

func TestValidate_FPSRange(t *testing.T) {
	tests := []struct {
		fps     int
		wantErr bool
	}{
		{0, true},
		{1, false},
		{30, false},
		{60, false},
		{120, false},
		{121, true},
	}
	for _, tt := range tests {
		cfg := Default()
		cfg.FPS = tt.fps
		err := cfg.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("fps=%d: wantErr=%v, got %v", tt.fps, tt.wantErr, err)
		}
	}
}

func TestValidate_BitrateRange(t *testing.T) {
	tests := []struct {
		bitrate int
		wantErr bool
	}{
		{99, true},
		{100, false},
		{4000, false},
		{50000, false},
		{50001, true},
	}
	for _, tt := range tests {
		cfg := Default()
		cfg.Bitrate = tt.bitrate
		err := cfg.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("bitrate=%d: wantErr=%v, got %v", tt.bitrate, tt.wantErr, err)
		}
	}
}

func TestValidate_Encoder(t *testing.T) {
	tests := []struct {
		encoder EncoderType
		wantErr bool
	}{
		{EncoderAuto, false},
		{EncoderNVENC, false},
		{EncoderQSV, false},
		{EncoderAMF, false},
		{EncoderCPU, false},
		{"invalid", true},
		{"", true},
	}
	for _, tt := range tests {
		cfg := Default()
		cfg.Encoder = tt.encoder
		err := cfg.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("encoder=%q: wantErr=%v, got %v", tt.encoder, tt.wantErr, err)
		}
	}
}

func TestParseResolution(t *testing.T) {
	tests := []struct {
		input        string
		wantW, wantH int
		wantErr      bool
	}{
		{"1920x1080", 1920, 1080, false},
		{"1280x720", 1280, 720, false},
		{"3840x2160", 3840, 2160, false},
		{"", 0, 0, true},
		{"1920", 0, 0, true},
		{"axb", 0, 0, true},
		{"1920x", 0, 0, true},
		{"x1080", 0, 0, true},
		{"-1x1080", 0, 0, true},
		{"1920x0", 0, 0, true},
	}
	for _, tt := range tests {
		w, h, err := ParseResolution(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseResolution(%q): wantErr=%v, got %v", tt.input, tt.wantErr, err)
			continue
		}
		if !tt.wantErr && (w != tt.wantW || h != tt.wantH) {
			t.Errorf("ParseResolution(%q) = (%d, %d), want (%d, %d)", tt.input, w, h, tt.wantW, tt.wantH)
		}
	}
}

func TestValidate_Resolution(t *testing.T) {
	cfg := Default()
	cfg.Resolution = "1920x1080"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid resolution should pass: %v", err)
	}

	cfg.Resolution = "bad"
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid resolution should fail")
	}

	cfg.Resolution = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("empty resolution (native) should pass: %v", err)
	}
}
