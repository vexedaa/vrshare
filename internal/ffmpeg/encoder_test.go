package ffmpeg

import (
	"testing"
)

func TestResolveEncoder_ExplicitCPU(t *testing.T) {
	enc := ResolveEncoder("cpu", func(string) bool { return true })
	if enc != "cpu" {
		t.Errorf("explicit cpu should return cpu, got %q", enc)
	}
}

func TestResolveEncoder_ExplicitNVENC(t *testing.T) {
	enc := ResolveEncoder("nvenc", func(string) bool { return false })
	if enc != "nvenc" {
		t.Errorf("explicit nvenc should return nvenc, got %q", enc)
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
