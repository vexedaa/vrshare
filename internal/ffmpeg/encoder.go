package ffmpeg

import (
	"context"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"
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

// ProbeFFmpegEncoder returns a probe function that tests if a given encoder
// actually works by attempting a 1-frame test encode. This catches cases where
// the encoder is listed (e.g. h264_nvenc in the essentials build) but the
// hardware isn't present or drivers are too old.
func ProbeFFmpegEncoder(ffmpegPath string) ProbeFunc {
	// First get the list of available encoders (fast, no hardware needed)
	out, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").Output()
	if err != nil {
		return func(encoder string) bool { return false }
	}
	encoderList := string(out)

	return func(encoder string) bool {
		// Quick check: is it even listed?
		if !strings.Contains(encoderList, encoder) {
			return false
		}
		// CPU encoders don't need hardware — listing is sufficient
		if encoder == "libx264" {
			return true
		}
		// GPU encoders: test-encode 1 frame to verify hardware works
		return testEncode(ffmpegPath, encoder)
	}
}

// testEncode runs a minimal 1-frame encode to verify the encoder works.
func testEncode(ffmpegPath, encoder string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=black:size=256x256:duration=0.04:rate=25",
		"-c:v", encoder,
		"-frames:v", "1",
		"-f", "null", "-",
	)
	err := cmd.Run()
	if err != nil {
		log.Printf("Encoder probe: %s failed: %v", encoder, err)
	}
	return err == nil
}
