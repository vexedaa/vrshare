package audio

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestIsSystemAudio(t *testing.T) {
	for _, dev := range []string{"", "Default Output Device", "default output device"} {
		if !IsSystemAudio(dev) {
			t.Errorf("IsSystemAudio(%q) = false, want true (legacy configs saved this)", dev)
		}
	}
	if IsSystemAudio("{0.0.0.00000000}.{1234}") {
		t.Error("an endpoint ID must select that device, not system audio")
	}
}

func TestListOutputDevices(t *testing.T) {
	devices, err := ListOutputDevices()
	if err != nil {
		t.Fatalf("ListOutputDevices: %v", err)
	}
	if len(devices) == 0 {
		t.Skip("no active output devices on this machine")
	}
	defaults := 0
	for _, d := range devices {
		t.Logf("device: %q id=%s default=%v", d.Name, d.ID, d.IsDefault)
		if d.ID == "" || d.Name == "" {
			t.Errorf("device missing ID or name: %+v", d)
		}
		if d.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Errorf("got %d default devices, want exactly 1", defaults)
	}
}

// countingWriter records how many bytes arrived and whether any were non-zero.
type countingWriter struct {
	mu      sync.Mutex
	n       int
	nonZero bool
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.n += len(p)
	for _, b := range p {
		if b != 0 {
			w.nonZero = true
			break
		}
	}
	return len(p), nil
}

func (w *countingWriter) stats() (int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.n, w.nonZero
}

// captureFor runs a capturer on dev for d and returns bytes delivered.
func captureFor(t *testing.T, dev string, d time.Duration) (int, bool) {
	t.Helper()
	w := &countingWriter{}
	c := NewCapturer(w)
	c.SetDevice(dev)
	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)
	time.Sleep(d)
	cancel()
	time.Sleep(100 * time.Millisecond)
	return w.stats()
}

// Endpoint loopback only produces packets while something is playing on the
// device, and FFmpeg stalls the whole stream while its audio pipe is starved.
// Capturing a specific device must therefore deliver a continuous real-time
// stream even when the device is silent (issue #2). Every device is checked
// because only an idle one exposes the problem; without the keep-alive render
// stream, idle devices delivered 0 bytes here.
func TestCapturerSpecificDeviceDeliversContinuousAudio(t *testing.T) {
	devices, err := ListOutputDevices()
	if err != nil || len(devices) == 0 {
		t.Skip("no output devices to capture")
	}

	const window = 1000 * time.Millisecond
	want := int(window.Seconds() * sampleRate * bytesPerFrame)
	for _, dev := range devices {
		got, _ := captureFor(t, dev.ID, window)
		t.Logf("captured %d bytes from %q in %v (real-time would be %d)", got, dev.Name, window, want)
		if got < want/2 {
			t.Errorf("%q: captured %d bytes, want at least %d — device loopback starves the pipe during silence", dev.Name, got, want/2)
		}
	}
}

// A saved device that is no longer connected must not silently fall back to
// capturing everything (which could stream audio the user chose to exclude),
// and must not starve FFmpeg either: it should feed silence until it returns.
func TestCapturerMissingDeviceFeedsSilence(t *testing.T) {
	got, nonZero := captureFor(t, "{0.0.0.00000000}.{00000000-0000-0000-0000-000000000000}", 1200*time.Millisecond)
	if got == 0 {
		t.Fatal("missing device delivered no audio at all — FFmpeg would stall")
	}
	if nonZero {
		t.Error("missing device delivered non-silent audio — it fell back to capturing something else")
	}
}
