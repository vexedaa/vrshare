package ffmpeg

import (
	"strings"
	"testing"
)

func TestResolveEncoder_ExplicitCPU(t *testing.T) {
	enc := ResolveEncoder("cpu", func(string) bool { return true })
	if enc != "cpu" {
		t.Errorf("explicit cpu should return cpu, got %q", enc)
	}
}

func TestResolveEncoder_ExplicitNVENC(t *testing.T) {
	enc := ResolveEncoder("nvenc", func(e string) bool { return e == "h264_nvenc" })
	if enc != "nvenc" {
		t.Errorf("explicit nvenc should return nvenc, got %q", enc)
	}
}

// An explicitly chosen GPU encoder that fails its probe (e.g. NVENC picked on
// an AMD-only machine — the bundled FFmpeg lists every vendor's encoder) must
// not be used as-is: FFmpeg would crash-loop before falling back to CPU, and
// the resulting lag shows up as a large audio delay (issue #3).
func TestResolveEncoder_ExplicitUnavailableFallsBackToWorkingGPU(t *testing.T) {
	enc := ResolveEncoder("nvenc", func(e string) bool { return e == "h264_amf" })
	if enc != "amf" {
		t.Errorf("unavailable explicit nvenc should fall back to working amf, got %q", enc)
	}
}

func TestResolveEncoder_ExplicitUnavailableNoGPUFallsBackToCPU(t *testing.T) {
	enc := ResolveEncoder("qsv", func(string) bool { return false })
	if enc != "cpu" {
		t.Errorf("unavailable explicit qsv with no GPU should fall back to cpu, got %q", enc)
	}
}

func TestCachedProbe_ProbesEachEncoderOnce(t *testing.T) {
	calls := map[string]int{}
	probe := CachedProbe(func(e string) bool {
		calls[e]++
		return e == "h264_amf"
	})
	// Explicit nvenc fails its probe, then auto resolution re-checks nvenc.
	ResolveEncoder("nvenc", probe)
	if calls["h264_nvenc"] != 1 {
		t.Errorf("h264_nvenc probed %d times, want 1 (test-encodes are slow)", calls["h264_nvenc"])
	}
}

func TestResolveEncoder_AutoDetectsNVENC(t *testing.T) {
	probe := func(encoder string) bool {
		return encoder == "h264_nvenc"
	}
	enc := ResolveEncoder("auto", probe)
	if enc != "nvenc" {
		t.Errorf("auto should detect nvenc, got %q", enc)
	}
}

func TestResolveEncoder_AutoDetectsQSV(t *testing.T) {
	probe := func(encoder string) bool {
		return encoder == "h264_qsv"
	}
	enc := ResolveEncoder("auto", probe)
	if enc != "qsv" {
		t.Errorf("auto should detect qsv, got %q", enc)
	}
}

func TestResolveEncoder_AutoDetectsAMF(t *testing.T) {
	probe := func(encoder string) bool {
		return encoder == "h264_amf"
	}
	enc := ResolveEncoder("auto", probe)
	if enc != "amf" {
		t.Errorf("auto should detect amf, got %q", enc)
	}
}

func TestResolveEncoder_AutoFallsToCPU(t *testing.T) {
	probe := func(encoder string) bool {
		return false
	}
	enc := ResolveEncoder("auto", probe)
	if enc != "cpu" {
		t.Errorf("auto with no hw should fall back to cpu, got %q", enc)
	}
}

func TestResolveEncoder_AutoPriority(t *testing.T) {
	probe := func(encoder string) bool { return true }
	enc := ResolveEncoder("auto", probe)
	if enc != "nvenc" {
		t.Errorf("auto with all available should pick nvenc, got %q", enc)
	}
}

func TestProbeDDAgrab_Available(t *testing.T) {
	probe := func(output string) bool {
		return strings.Contains(output, "ddagrab")
	}
	if !probe("  D  ddagrab           Desktop Duplication API") {
		t.Error("should detect ddagrab in devices output")
	}
}

func TestProbeDDAgrab_NotAvailable(t *testing.T) {
	probe := func(output string) bool {
		return strings.Contains(output, "ddagrab")
	}
	if probe("  D  gdigrab           GDI API Windows frame grabber") {
		t.Error("should not detect ddagrab when not listed")
	}
}
