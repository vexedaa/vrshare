package config

import (
	"fmt"
	"strconv"
	"strings"
)

type EncoderType string

const (
	EncoderAuto  EncoderType = "auto"
	EncoderNVENC EncoderType = "nvenc"
	EncoderQSV   EncoderType = "qsv"
	EncoderAMF   EncoderType = "amf"
	EncoderCPU   EncoderType = "cpu"
)

type Config struct {
	Port        int         `json:"port"`
	Monitor     int         `json:"monitor"`
	FPS         int         `json:"fps"`
	Resolution  string      `json:"resolution"`
	Bitrate     int         `json:"bitrate"`
	Encoder     EncoderType `json:"encoder"`
	Audio       bool        `json:"audio"`
	AudioGain   int         `json:"audioGain"` // dB boost, 0 = no change, default 6
	AudioDevice string      `json:"audioDevice"`
	Tunnel      string      `json:"tunnel"`
}

func Default() Config {
	return Config{
		Port:      8080,
		FPS:       30,
		Bitrate:   4000,
		Encoder:   EncoderAuto,
		Audio:     false,
		AudioGain: 6,
		Tunnel:    "",
	}
}

func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", c.Port)
	}
	if c.FPS < 1 || c.FPS > 120 {
		return fmt.Errorf("fps must be between 1 and 120, got %d", c.FPS)
	}
	if c.Bitrate < 100 || c.Bitrate > 50000 {
		return fmt.Errorf("bitrate must be between 100 and 50000 kbps, got %d", c.Bitrate)
	}
	if c.Monitor < 0 {
		return fmt.Errorf("monitor must be >= 0, got %d", c.Monitor)
	}
	if c.AudioGain < -20 || c.AudioGain > 30 {
		return fmt.Errorf("audio gain must be between -20 and 30 dB, got %d", c.AudioGain)
	}
	if c.Resolution != "" {
		if _, _, err := ParseResolution(c.Resolution); err != nil {
			return err
		}
	}
	switch c.Encoder {
	case EncoderAuto, EncoderNVENC, EncoderQSV, EncoderAMF, EncoderCPU:
	default:
		return fmt.Errorf("encoder must be one of: auto, nvenc, qsv, amf, cpu; got %q", c.Encoder)
	}
	if c.Tunnel != "" && c.Tunnel != "cloudflare" && c.Tunnel != "tailscale" {
		return fmt.Errorf("tunnel must be empty, \"cloudflare\", or \"tailscale\"")
	}
	return nil
}

func ParseResolution(s string) (width, height int, err error) {
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("resolution must be WxH (e.g., 1920x1080), got %q", s)
	}
	width, err = strconv.Atoi(parts[0])
	if err != nil || width < 1 {
		return 0, 0, fmt.Errorf("invalid resolution width: %q", parts[0])
	}
	height, err = strconv.Atoi(parts[1])
	if err != nil || height < 1 {
		return 0, 0, fmt.Errorf("invalid resolution height: %q", parts[1])
	}
	return width, height, nil
}
