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

// TestAsyncWriterForwardsImmediately verifies that audio reaches the underlying
// writer without any priming signal. FFmpeg stalls its entire pipeline — it
// emits no encoded frames at all — when its mapped pipe:0 audio input is
// starved, so audio MUST flow from the very first write or the stream
// deadlocks. (Regression guard for the first-frame audio gate.)
func TestAsyncWriterForwardsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dst := &writeCloser{}
	aw := NewAsyncWriter(ctx, dst, 64)

	want := []byte("audio-from-the-start")
	aw.Write(want)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(dst.Bytes(), want) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := dst.Bytes(); !bytes.Contains(got, want) {
		t.Fatalf("audio not forwarded without a priming signal; got %q", got)
	}
}

// TestAsyncWriterContextCancel verifies that cancelling the context does not
// deadlock and closes the underlying writer.
func TestAsyncWriterContextCancel(t *testing.T) {
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

// TestAsyncWriterPassthrough verifies normal pass-through to a blocking writer,
// using an io.Pipe so reads block until data arrives.
func TestAsyncWriterPassthrough(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pr, pw := io.Pipe()
	aw := NewAsyncWriter(ctx, pw, 32)

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
		t.Fatal("timeout waiting for audio data")
	}
}
