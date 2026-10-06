package audio

import (
	"bytes"
	"context"
	"io"
	"log"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

const (
	audclntSBufferEmpty       = 0x08890001 // success: no data available yet
	audclntEDeviceInvalidated = 0x88890004 // failure: endpoint unplugged/disabled
	eNotImpl                  = 0x80004001
)

// fakeCaptureClient is a minimal IAudioCaptureClient COM object whose
// GetBuffer returns a fixed HRESULT, for driving readBuffers without audio
// hardware. Its callbacks never write through their pointer arguments: those
// point into the caller's stack, which may move while a Go callback runs.
type fakeCaptureClient struct {
	vtbl *[6]uintptr
}

var (
	fakeVtblOnce    sync.Once
	fakeVtbl        [6]uintptr
	fakeGetBufferHR uintptr
	fakeClients     []*fakeCaptureClient // keeps fakes reachable while COM pointers to them exist
)

func newFakeCaptureClient(getBufferHR uintptr) uintptr {
	fakeVtblOnce.Do(func() {
		notImpl := syscall.NewCallback(func(this uintptr) uintptr { return eNotImpl })
		getBuffer := syscall.NewCallback(func(this, data, frames, flags, devPos, qpcPos uintptr) uintptr {
			return fakeGetBufferHR
		})
		// 0-2 IUnknown, 3 GetBuffer, 4 ReleaseBuffer, 5 GetNextPacketSize
		fakeVtbl = [6]uintptr{notImpl, notImpl, notImpl, getBuffer, notImpl, notImpl}
	})
	fakeGetBufferHR = getBufferHR
	fc := &fakeCaptureClient{vtbl: &fakeVtbl}
	fakeClients = append(fakeClients, fc)
	return uintptr(unsafe.Pointer(fc))
}

// When the captured device is unplugged or disabled, GetBuffer fails with
// AUDCLNT_E_DEVICE_INVALIDATED on every call. readBuffers must report that so
// the session ends and the loop falls back to silence + retry; treating it like
// "no data yet" spins forever writing nothing, which stalls FFmpeg.
func TestReadBuffersReportsDeviceLoss(t *testing.T) {
	c := NewCapturer(io.Discard)

	if err := c.readBuffers(newFakeCaptureClient(audclntEDeviceInvalidated)); err == nil {
		t.Error("readBuffers returned nil for AUDCLNT_E_DEVICE_INVALIDATED; the session would never end")
	}
	if err := c.readBuffers(newFakeCaptureClient(audclntSBufferEmpty)); err != nil {
		t.Errorf("readBuffers(AUDCLNT_S_BUFFER_EMPTY) = %v, want nil — an empty buffer is normal", err)
	}
}

// Silence padding must track the wall clock. Writing a whole retry interval's
// worth up front leaves audio that far ahead of real time, and once capture
// resumes the excess becomes a permanent delay of audio behind video.
func TestPadSilenceTracksWallClock(t *testing.T) {
	w := &countingWriter{}
	c := NewCapturer(w)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := c.padSilence(ctx, 2*time.Second); err != nil {
		t.Fatalf("padSilence: %v", err)
	}
	elapsed := time.Since(start)

	got, nonZero := w.stats()
	bytesFor := func(d time.Duration) int { return int(d.Seconds()*sampleRate) * bytesPerFrame }
	if elapsed > time.Second {
		t.Fatalf("padSilence ran %v after its context was cancelled at 200ms", elapsed)
	}
	if max := bytesFor(elapsed + 20*time.Millisecond); got > max {
		t.Errorf("padded %d bytes in %v; real time allows at most %d — silence ran ahead of the clock", got, elapsed, max)
	}
	if min := bytesFor(elapsed - 50*time.Millisecond); got < min {
		t.Errorf("padded only %d bytes in %v, want at least %d — FFmpeg would starve", got, elapsed, min)
	}
	if nonZero {
		t.Error("padding wrote non-zero samples")
	}
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Switching source must take effect promptly even while the capture loop is
// between sessions (activating, or waiting to retry a missing device). The
// switch used to cancel only an already-running session, so one that landed in
// that window was lost or delayed and the old source kept streaming.
func TestSetDeviceSwitchesDuringRetryWait(t *testing.T) {
	devices, err := ListOutputDevices()
	if err != nil || len(devices) == 0 {
		t.Skip("no output devices to switch to")
	}

	logs := &syncBuffer{}
	orig := log.Writer()
	log.SetOutput(logs)
	defer log.SetOutput(orig)

	c := NewCapturer(io.Discard)
	c.SetDevice("{0.0.0.00000000}.{00000000-0000-0000-0000-000000000000}") // not connected
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	time.Sleep(150 * time.Millisecond) // now inside the 1s retry wait
	c.SetDevice(devices[0].ID)

	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) && !strings.Contains(logs.String(), "Audio: capturing output device") {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "Audio: capturing output device") {
		t.Errorf("device switch didn't take effect within 400ms of SetDevice; log:\n%s", logs.String())
	}
}
