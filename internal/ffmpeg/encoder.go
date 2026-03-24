package ffmpeg

import (
	"os/exec"
	"strings"
)

type ProbeFunc func(encoder string) bool

func ResolveEncoder(encoder string, probe ProbeFunc) string {
	if encoder != "auto" {
		return encoder
	}

	priority := []struct {
		name    string
		ffCodec string
	}{
		{"nvenc", "h264_nvenc"},
		{"qsv", "h264_qsv"},
		{"amf", "h264_amf"},
	}

	for _, p := range priority {
		if probe(p.ffCodec) {
			return p.name
		}
	}

	return "cpu"
}

func ProbeFFmpegEncoder(ffmpegPath string) ProbeFunc {
	return func(encoder string) bool {
		out, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").Output()
		if err != nil {
			return false
		}
		return strings.Contains(string(out), encoder)
	}
}
