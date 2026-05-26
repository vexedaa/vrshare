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
// Before SignalReady is called, all Write calls are silently dropped. This
// eliminates the startup A/V sync offset that occurs when WASAPI activates
// faster than the FFmpeg process: audio captured before FFmpeg's first encoded
// frame is discarded so that audio PTS=0 aligns with video PTS=0.
//
// Call SignalReady when FFmpeg confirms its first encoded frame (detected from
// its stderr progress output). After that, writes flow normally.
//
// A background goroutine drains buffered data to the underlying writer. The
// writer is closed when the context is cancelled or a pipe write fails.
type AsyncWriter struct {
	ch              chan []byte
	w               io.WriteCloser
	done            chan struct{}
	ready           atomic.Int32 // 0 = drop writes; 1 = accept writes
	droppedPreReady atomic.Int64 // stale chunks dropped before SignalReady
	werr            error
	mu              sync.Mutex
	dropped         atomic.Int64 // chunks dropped due to full channel
}

// NewAsyncWriter wraps w with a buffered channel of bufSlots entries. Each
// entry holds one audio chunk (~1920 bytes at 10ms / 48kHz stereo 16-bit).
// 256 slots ≈ 2.5 seconds of buffer. The underlying writer is closed when
// ctx is cancelled or a write to it fails.
//
// All writes are silently dropped until SignalReady is called.
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

// SignalReady enables audio delivery. All audio written before this call has
// been silently dropped. Audio written after this call is buffered and
// forwarded to FFmpeg's stdin, so audio PTS=0 corresponds to "now" — the same
// real-world moment as video PTS=0. Safe to call multiple times.
func (aw *AsyncWriter) SignalReady() {
	if aw.ready.CompareAndSwap(0, 1) {
		if n := aw.droppedPreReady.Load(); n > 0 {
			log.Printf("Audio: discarded %d pre-buffer chunks (~%dms) — A/V sync aligned",
				n, n*10)
		} else {
			log.Println("Audio: drain enabled (no pre-buffer to discard)")
		}
	}
}

// Write copies p into the internal buffer. It never blocks. Before
// SignalReady is called, writes are silently dropped. Returns an error
// only if the drain goroutine has encountered a fatal pipe failure.
func (aw *AsyncWriter) Write(p []byte) (int, error) {
	if aw.ready.Load() == 0 {
		// Drop audio captured before FFmpeg's first frame — it is stale
		// relative to the video timeline and would shift audio PTS=0 earlier
		// than video PTS=0, causing audio to lead video on playback.
		aw.droppedPreReady.Add(1)
		return len(p), nil
	}

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

// logDrops periodically reports how many audio chunks were dropped due to the
// channel being full (i.e. FFmpeg reading too slowly).
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
