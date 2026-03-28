package audio

import (
	"context"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// AsyncWriter buffers audio data between the WASAPI capturer and the OS pipe
// to FFmpeg so that capturer writes never block. If the internal buffer fills
// (FFmpeg is reading too slowly), new audio chunks are dropped to prevent
// WASAPI buffer overflow.
//
// A background goroutine drains buffered data to the underlying writer. The
// writer is closed when the context is cancelled or a pipe write fails.
type AsyncWriter struct {
	ch      chan []byte
	w       io.WriteCloser
	done    chan struct{}
	werr    error
	mu      sync.Mutex
	dropped atomic.Int64
}

// NewAsyncWriter wraps w with a buffered channel of bufSlots entries. Each
// entry holds one audio chunk (~1920 bytes at 10ms / 48kHz stereo 16-bit).
// 256 slots ≈ 2.5 seconds of buffer. The underlying writer is closed when
// ctx is cancelled or a write to it fails.
func NewAsyncWriter(ctx context.Context, w io.WriteCloser, bufSlots int) *AsyncWriter {
	aw := &AsyncWriter{
		ch:   make(chan []byte, bufSlots),
		w:    w,
		done: make(chan struct{}),
	}
	go aw.drain(ctx)
	go aw.logDrops(ctx)
	return aw
}

// Write copies p into the internal buffer. It never blocks. Returns an error
// only if the drain goroutine has encountered a fatal pipe failure.
func (aw *AsyncWriter) Write(p []byte) (int, error) {
	aw.mu.Lock()
	err := aw.werr
	aw.mu.Unlock()
	if err != nil {
		return 0, err
	}

	buf := make([]byte, len(p))
	copy(buf, p)

	select {
	case aw.ch <- buf:
	default:
		aw.dropped.Add(1)
	}
	return len(p), nil
}

func (aw *AsyncWriter) drain(ctx context.Context) {
	defer close(aw.done)
	defer aw.w.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case buf := <-aw.ch:
			if _, err := aw.w.Write(buf); err != nil {
				aw.mu.Lock()
				aw.werr = err
				aw.mu.Unlock()
				log.Printf("Audio: pipe write error: %v — audio delivery stopped", err)
				return
			}
		}
	}
}

// logDrops periodically reports how many audio chunks were dropped.
func (aw *AsyncWriter) logDrops(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n := aw.dropped.Swap(0); n > 0 {
				log.Printf("Audio: dropped %d chunks (FFmpeg not reading fast enough)", n)
			}
		}
	}
}
