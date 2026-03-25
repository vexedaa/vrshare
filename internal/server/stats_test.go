package server

import (
	"testing"
)

func TestParseStatsLine(t *testing.T) {
	line := "frame=  120 fps= 60.0 q=28.0 size=     384kB time=00:00:02.00 bitrate=1571.1kbits/s speed=1.00x"
	stats := parseStatsLine(line)

	if stats.FPS != 60.0 {
		t.Errorf("FPS = %f, want 60.0", stats.FPS)
	}
	if stats.Bitrate != 1571 {
		t.Errorf("Bitrate = %d, want 1571", stats.Bitrate)
	}
	if stats.Speed != 1.0 {
		t.Errorf("Speed = %f, want 1.0", stats.Speed)
	}
}

func TestParseStatsLineWithDrop(t *testing.T) {
	line := "frame=  500 fps= 29.9 q=30.0 size=    1024kB time=00:00:16.67 bitrate= 502.3kbits/s drop=3 speed=0.95x"
	stats := parseStatsLine(line)

	if stats.DroppedFrames != 3 {
		t.Errorf("DroppedFrames = %d, want 3", stats.DroppedFrames)
	}
	if stats.Speed != 0.95 {
		t.Errorf("Speed = %f, want 0.95", stats.Speed)
	}
}

func TestParseStatsLineNonProgress(t *testing.T) {
	line := "Press [q] to stop, [?] for help"
	stats := parseStatsLine(line)
	if stats.FPS != 0 {
		t.Errorf("non-progress line should return zero FPS")
	}
}

func TestStatsParserWrite(t *testing.T) {
	p := NewStatsParser(nil)
	input := "frame=  240 fps= 59.8 q=25.0 size=     768kB time=00:00:04.00 bitrate=1573.2kbits/s speed=0.98x\n"
	p.Write([]byte(input))

	s := p.Latest()
	if s.FPS != 59.8 {
		t.Errorf("FPS = %f, want 59.8", s.FPS)
	}
	if s.Speed != 0.98 {
		t.Errorf("Speed = %f, want 0.98", s.Speed)
	}
}

func TestStatsParserPassthrough(t *testing.T) {
	var buf []byte
	w := &sliceWriter{buf: &buf}
	p := NewStatsParser(w)
	input := []byte("some output\n")
	n, err := p.Write(input)
	if err != nil || n != len(input) {
		t.Errorf("passthrough failed: n=%d err=%v", n, err)
	}
	if string(*w.buf) != string(input) {
		t.Errorf("passthrough data mismatch")
	}
}

func TestStatsParserCarriageReturn(t *testing.T) {
	p := NewStatsParser(nil)
	input := "frame=  60 fps= 30.0 q=28.0 size=     192kB time=00:00:02.00 bitrate=786.0kbits/s speed=1.00x\r"
	p.Write([]byte(input))

	s := p.Latest()
	if s.FPS != 30.0 {
		t.Errorf("FPS = %f, want 30.0 (carriage return delimiter)", s.FPS)
	}
}

type sliceWriter struct{ buf *[]byte }

func (w *sliceWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}
