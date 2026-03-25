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

// StatsParser wraps an io.Writer and parses FFmpeg progress output.
// Handles both the classic stderr format and -progress pipe:2 key=value format.
// If inner is nil, output is discarded after parsing.
type StatsParser struct {
	inner  io.Writer
	mu     sync.Mutex
	latest EncodingStats
	buf    []byte
	// Accumulate key=value pairs between "progress=" lines
	pending EncodingStats
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
		if line == "" {
			continue
		}

		// Try key=value format first (-progress pipe:2)
		if strings.Contains(line, "=") && !strings.Contains(line, " ") {
			p.parseProgressLine(line)
			continue
		}

		// Try classic stderr format
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

func (p *StatsParser) parseProgressLine(line string) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return
	}
	key, val := parts[0], parts[1]

	switch key {
	case "fps":
		p.pending.FPS, _ = strconv.ParseFloat(val, 64)
	case "bitrate":
		val = strings.TrimSuffix(val, "kbits/s")
		val = strings.TrimSpace(val)
		f, _ := strconv.ParseFloat(val, 64)
		p.pending.Bitrate = int(f)
	case "speed":
		val = strings.TrimSuffix(val, "x")
		val = strings.TrimSpace(val)
		p.pending.Speed, _ = strconv.ParseFloat(val, 64)
	case "drop_frames":
		p.pending.DroppedFrames, _ = strconv.Atoi(val)
	case "progress":
		// "continue" or "end" — flush accumulated stats
		if p.pending.FPS > 0 || p.pending.Bitrate > 0 {
			p.mu.Lock()
			p.latest = p.pending
			p.mu.Unlock()
		}
		p.pending = EncodingStats{}
	}
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
