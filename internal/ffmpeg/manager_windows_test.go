//go:build windows

package ffmpeg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestMain lets this test binary stand in for ffmpeg.exe: started with
// VRSHARE_FAKE_FFMPEG=1 it records its own priority class (when asked to) and
// then "streams" until it is killed.
func TestMain(m *testing.M) {
	if os.Getenv("VRSHARE_FAKE_FFMPEG") == "1" {
		if out := os.Getenv("VRSHARE_FAKE_PRIORITY_FILE"); out != "" {
			self, _ := syscall.GetCurrentProcess()
			class, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetPriorityClass").Call(uintptr(self))
			os.WriteFile(out, []byte(fmt.Sprintf("%#x", class)), 0644)
		}
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// While a game loads the CPU, a normal-priority FFmpeg is starved for seconds
// at a time: with 14 of 16 hardware threads busy it dropped 638 audio chunks
// in a minute and fell behind real time; at above-normal priority it dropped
// none and kept up.
func TestFFmpegRunsAboveNormalPriority(t *testing.T) {
	t.Setenv("VRSHARE_FAKE_FFMPEG", "1")
	priorityFile := filepath.Join(t.TempDir(), "priority")
	t.Setenv("VRSHARE_FAKE_PRIORITY_FILE", priorityFile)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	m := NewManager(self, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx, nil, nil) }()
	defer func() { cancel(); <-done }()

	var got []byte
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if got, err = os.ReadFile(priorityFile); err == nil && len(got) > 0 {
			break
		}
	}
	const aboveNormal = "0x8000" // ABOVE_NORMAL_PRIORITY_CLASS
	if string(got) != aboveNormal {
		t.Errorf("FFmpeg priority class = %q, want %s (above normal)", got, aboveNormal)
	}
}
