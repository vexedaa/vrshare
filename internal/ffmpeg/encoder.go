package ffmpeg

import (
	"os/exec"
	"runtime"
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

// ProbeDDAgrab checks if FFmpeg supports the ddagrab filter (DXGI Desktop
// Duplication). ddagrab is a lavfi source filter, not an input device,
// so we check -filters rather than -devices.
func ProbeDDAgrab(ffmpegPath string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	out, err := exec.Command(ffmpegPath, "-hide_banner", "-filters").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "ddagrab")
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
