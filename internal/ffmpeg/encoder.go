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

// ProbeFFmpegEncoder runs ffmpeg -encoders once and returns a probe function
// that checks the cached output for each encoder name.
func ProbeFFmpegEncoder(ffmpegPath string) ProbeFunc {
	out, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").Output()
	if err != nil {
		return func(encoder string) bool { return false }
	}
	encoderList := string(out)
	return func(encoder string) bool {
		return strings.Contains(encoderList, encoder)
	}
}
