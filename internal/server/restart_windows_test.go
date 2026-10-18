//go:build windows

package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/vexedaa/vrshare/internal/config"
)

// TestMain lets this test binary stand in for ffmpeg.exe: when started with
// VRSHARE_FAKE_FFMPEG=1 it answers the encoder listing (with no hardware
// encoders, so resolution picks "cpu" without test-encodes) and otherwise
// "streams" until it is killed, like a real capture.
func TestMain(m *testing.M) {
	if os.Getenv("VRSHARE_FAKE_FFMPEG") == "1" {
		for _, a := range os.Args[1:] {
			if a == "-encoders" {
				os.Exit(0)
			}
		}
		if dir := os.Getenv("VRSHARE_FAKE_ARGS_DIR"); dir != "" {
			os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.args", os.Getpid())), []byte(strings.Join(os.Args[1:], "\n")), 0644)
		}
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runningFakeFFmpegs counts this test process's fake-FFmpeg children, waiting
// for the count to settle (encoder-listing probes exit within milliseconds).
func runningFakeFFmpegs(t *testing.T) int {
	t.Helper()
	self := strings.ToLower(filepath.Base(os.Args[0]))
	count := func() int {
		n := 0
		for _, p := range listProcesses() {
			if p.ParentPID == uint32(os.Getpid()) && strings.ToLower(syscall.UTF16ToString(p.ExeFile[:])) == self {
				n++
			}
		}
		return n
	}
	last, stableSince := count(), time.Now()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if n := count(); n != last {
			last, stableSince = n, time.Now()
		} else if time.Since(stableSince) > 500*time.Millisecond {
			break
		}
	}
	return last
}

// Two restarts requested close together (a double-click, or Restart clicked
// while a slow restart is still running) both waited on the same FFmpeg exit
// and then each launched a new FFmpeg. The second overwrote the first's cancel
// func, orphaning a full capture+encode process that kept running — reading
// the shared audio pipe and writing the same segment files — until Stop.
// streamingWithFakeFFmpeg returns a server in the state Start leaves it in,
// with one fake FFmpeg running, and stops it when the test ends.
func streamingWithFakeFFmpeg(t *testing.T) *Server {
	t.Helper()
	t.Setenv("VRSHARE_FAKE_FFMPEG", "1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	s := New(config.Default())
	s.ffmpegPath = self
	s.segDir = t.TempDir()
	s.encoder = "cpu"
	s.tsBase = time.Now()
	s.srvCtx, s.srvCancel = context.WithCancel(context.Background())
	s.status = "streaming"
	t.Cleanup(func() {
		s.Stop()
		if n := runningFakeFFmpegs(t); n != 0 {
			t.Errorf("%d fake FFmpeg process(es) still running after Stop", n)
		}
	})

	if err := s.startFFmpeg(); err != nil {
		t.Fatal(err)
	}
	if n := runningFakeFFmpegs(t); n != 1 {
		t.Fatalf("%d FFmpeg processes after start, want 1", n)
	}
	return s
}

// Every FFmpeg in a session must rebase timestamps to the same session start.
// With a per-launch base, each restart would send viewers' players back to
// near zero, and each player would have to resynchronise across the jump.
func TestRestartKeepsSessionTimestampBase(t *testing.T) {
	argsDir := t.TempDir()
	t.Setenv("VRSHARE_FAKE_ARGS_DIR", argsDir)
	s := streamingWithFakeFFmpeg(t)
	time.Sleep(1100 * time.Millisecond) // a per-launch base would now differ by over a second
	if err := s.RestartCapture(); err != nil {
		t.Fatal(err)
	}
	runningFakeFFmpegs(t) // let the relaunch record its args

	files, _ := filepath.Glob(filepath.Join(argsDir, "*.args"))
	var offsets []string
	for _, f := range files {
		b, _ := os.ReadFile(f)
		args := strings.Split(string(b), "\n")
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-output_ts_offset" {
				offsets = append(offsets, args[i+1])
			}
		}
	}
	if len(offsets) != 2 || offsets[0] != offsets[1] {
		t.Errorf("-output_ts_offset per launch = %q, want the same value for both launches", offsets)
	}
}

func TestOverlappingRestartsLeaveOneFFmpeg(t *testing.T) {
	s := streamingWithFakeFFmpeg(t)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s.RestartCapture()
		}()
	}
	close(start)
	wg.Wait()

	if n := runningFakeFFmpegs(t); n != 1 {
		t.Errorf("%d FFmpeg processes running after two overlapping restarts, want 1", n)
	}
}
