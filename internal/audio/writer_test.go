package audio

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

// writeCloser wraps a *bytes.Buffer and satisfies io.WriteCloser.
type writeCloser struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed bool
}

func (w *writeCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *writeCloser) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func (w *writeCloser) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	b := make([]byte, w.buf.Len())
	copy(b, w.buf.Bytes())
	return b
}

// TestAsyncWriterDropsBeforeReady verifies that Write calls are silently
// dropped before SignalReady, and that no data reaches the underlying writer.
func TestAsyncWriterDropsBeforeReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dst := &writeCloser{}
	aw := NewAsyncWriter(ctx, dst, 64)

	stale := []byte("stale-audio")
	for i := 0; i < 5; i++ {
		n, err := aw.Write(stale)
		if n != len(stale) || err != nil {
			t.Fatalf("Write before ready: n=%d err=%v; want n=%d err=nil", n, err, len(stale))
		}
	}

	// Give the drain goroutine time to process — it should have nothing to do.
	time.Sleep(50 * time.Millisecond)

	if got := dst.Bytes(); len(got) != 0 {
		t.Fatalf("data reached writer before SignalReady: %q", got)
	}
}

// TestAsyncWriterDeliversAfterReady verifies that writes after SignalReady
// are forwarded to the underlying writer.
func TestAsyncWriterDeliversAfterReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dst := &writeCloser{}
	aw := NewAsyncWriter(ctx, dst, 64)

	// Pre-signal writes — must be dropped.
	aw.Write([]byte("stale"))

	aw.SignalReady()

	fresh := []byte("fresh-audio-data")
	aw.Write(fresh)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(dst.Bytes(), fresh) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got := dst.Bytes()
	if !bytes.Contains(got, fresh) {
		t.Fatalf("fresh audio not delivered after SignalReady; got %q", got)
	}
	if bytes.Contains(got, []byte("stale")) {
		t.Fatalf("stale audio reached the writer — it should have been dropped")
	}
}

// TestAsyncWriterSignalReadyIdempotent verifies that multiple SignalReady
// calls do not panic.
func TestAsyncWriterSignalReadyIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dst := &writeCloser{}
	aw := NewAsyncWriter(ctx, dst, 64)

	aw.SignalReady()
	aw.SignalReady()
	aw.SignalReady()
}

// TestAsyncWriterContextCancelWithoutSignal verifies that cancelling the
// context without ever calling SignalReady does not deadlock and closes the
// underlying writer.
func TestAsyncWriterContextCancelWithoutSignal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	dst := &writeCloser{}
	aw := NewAsyncWriter(ctx, dst, 8)

	for i := 0; i < 5; i++ {
		n, err := aw.Write([]byte{1, 2, 3})
		if n != 3 || err != nil {
			t.Fatalf("Write returned n=%d err=%v; want n=3 err=nil", n, err)
		}
	}

	<-ctx.Done()
	// Allow goroutines to clean up.
	time.Sleep(50 * time.Millisecond)

	dst.mu.Lock()
	closed := dst.closed
	dst.mu.Unlock()
	if !closed {
		t.Error("underlying writer was not closed after context cancellation")
	}
}

// TestAsyncWriterWriteAfterSignal verifies normal pass-through once ready,
// using an io.Pipe so reads block until data arrives.
func TestAsyncWriterWriteAfterSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pr, pw := io.Pipe()
	aw := NewAsyncWriter(ctx, pw, 32)
	aw.SignalReady()

	want := []byte("hello-audio")
	aw.Write(want)

	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, len(want))
		_, err := io.ReadFull(pr, buf)
		ch <- result{buf, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("ReadFull: %v", r.err)
		}
		if !bytes.Equal(r.data, want) {
			t.Fatalf("got %q; want %q", r.data, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for audio data after SignalReady")
	}
}
