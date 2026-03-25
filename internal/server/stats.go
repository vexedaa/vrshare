package server

import (
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// EncodingStats holds parsed FFmpeg encoding statistics.
type EncodingStats struct {
	FPS           float64
	Bitrate       int     // kbps
	DroppedFrames int
	Speed         float64 // encoding speed multiplier (1.0 = realtime)
}

// StatsParser wraps an io.Writer and parses FFmpeg stderr progress lines.
// If inner is nil, stderr output is discarded after parsing.
type StatsParser struct {
	inner  io.Writer
	mu     sync.Mutex
	latest EncodingStats
	buf    []byte
}

func NewStatsParser(inner io.Writer) *StatsParser {
	return &StatsParser{inner: inner}
}

func (p *StatsParser) Write(data []byte) (int, error) {
	p.buf = append(p.buf, data...)
	for {
		idx := indexOfAny(p.buf, '\n', '\r')
		if idx < 0 {
			break
		}
		line := string(p.buf[:idx])
		p.buf = p.buf[idx+1:]

		stats := parseStatsLine(line)
		if stats.FPS > 0 || stats.Bitrate > 0 {
			p.mu.Lock()
			p.latest = stats
			p.mu.Unlock()
		}
	}

	if p.inner != nil {
		return p.inner.Write(data)
	}
	return len(data), nil
}

func (p *StatsParser) Latest() EncodingStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.latest
}

var (
	reFPS     = regexp.MustCompile(`fps=\s*([\d.]+)`)
	reBitrate = regexp.MustCompile(`bitrate=\s*([\d.]+)kbits/s`)
	reSpeed   = regexp.MustCompile(`speed=\s*([\d.]+)x`)
	reDrop    = regexp.MustCompile(`drop=\s*(\d+)`)
)

func parseStatsLine(line string) EncodingStats {
	if !strings.Contains(line, "frame=") {
		return EncodingStats{}
	}
	var s EncodingStats
	if m := reFPS.FindStringSubmatch(line); len(m) > 1 {
		s.FPS, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reBitrate.FindStringSubmatch(line); len(m) > 1 {
		f, _ := strconv.ParseFloat(m[1], 64)
		s.Bitrate = int(f)
	}
	if m := reSpeed.FindStringSubmatch(line); len(m) > 1 {
		s.Speed, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reDrop.FindStringSubmatch(line); len(m) > 1 {
		s.DroppedFrames, _ = strconv.Atoi(m[1])
	}
	return s
}

func indexOfAny(b []byte, c1, c2 byte) int {
	for i, v := range b {
		if v == c1 || v == c2 {
			return i
		}
	}
	return -1
}
