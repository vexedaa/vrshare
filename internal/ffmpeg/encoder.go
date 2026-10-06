package ffmpeg

import (
	"context"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

type ProbeFunc func(encoder string) bool

// gpuEncoders lists the hardware encoders in auto-detection priority order.
var gpuEncoders = []struct {
	name    string
	ffCodec string
}{
	{"nvenc", "h264_nvenc"},
	{"qsv", "h264_qsv"},
	{"amf", "h264_amf"},
}

// ResolveEncoder maps a configured encoder ("auto", "nvenc", "qsv", "amf",
// "cpu") to one that actually works on this machine. "auto" picks the first
// GPU encoder that passes the probe. An explicitly chosen GPU encoder is used
// only if it passes the probe too; otherwise it is resolved like "auto".
// FFmpeg builds list every vendor's encoder regardless of the installed GPU,
// so honouring a dead choice would crash-loop FFmpeg until the CPU fallback
// kicked in (issue #3: NVENC selected on an AMD machine).
func ResolveEncoder(encoder string, probe ProbeFunc) string {
	if encoder == "cpu" {
		return "cpu"
	}

	for _, p := range gpuEncoders {
		if p.name == encoder && probe(p.ffCodec) {
			return p.name
		}
	}

	for _, p := range gpuEncoders {
		if probe(p.ffCodec) {
			return p.name
		}
	}

	return "cpu"
}

// CachedProbe memoizes probe results so each encoder is test-encoded at most
// once, however many times resolution asks about it. Different encoders can be
// probed concurrently.
func CachedProbe(probe ProbeFunc) ProbeFunc {
	type result struct {
		once sync.Once
		ok   bool
	}
	var mu sync.Mutex
	results := map[string]*result{}
	return func(encoder string) bool {
		mu.Lock()
		r, seen := results[encoder]
		if !seen {
			r = &result{}
			results[encoder] = r
		}
		mu.Unlock()
		r.once.Do(func() { r.ok = probe(encoder) })
		return r.ok
	}
}

// ProbeDDAgrab checks if FFmpeg supports the ddagrab filter (DXGI Desktop
// Duplication). ddagrab is a lavfi source filter, not an input device,
// so we check -filters rather than -devices.
func ProbeDDAgrab(ffmpegPath string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	cmd := exec.Command(ffmpegPath, "-hide_banner", "-filters")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "ddagrab")
}

// hideWindow stops a probe process from flashing a console window when VRShare
// runs as a GUI (windowsgui) app.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// ProbeFFmpegEncoder returns a probe function that tests if a given encoder
// actually works by attempting a 1-frame test encode. This catches cases where
// the encoder is listed (e.g. h264_nvenc in the essentials build) but the
// hardware isn't present or drivers are too old. Results are cached, so the
// returned probe test-encodes each encoder at most once.
func ProbeFFmpegEncoder(ffmpegPath string) ProbeFunc {
	// First get the list of available encoders (fast, no hardware needed)
	listCmd := exec.Command(ffmpegPath, "-hide_banner", "-encoders")
	hideWindow(listCmd)
	out, err := listCmd.Output()
	if err != nil {
		return func(encoder string) bool { return false }
	}
	encoderList := string(out)

	return CachedProbe(func(encoder string) bool {
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
	})
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
	hideWindow(cmd)
	err := cmd.Run()
	if err != nil {
		log.Printf("Encoder probe: %s failed: %v", encoder, err)
	}
	return err == nil
}
