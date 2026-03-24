# ddagrab Capture + Test Player Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Switch Windows screen capture from gdigrab to ddagrab for dramatically lower resource usage, and add a built-in browser test player page.

**Architecture:** Update `BuildArgs` to accept a `useDDAgrab` flag. When true, use `-f ddagrab` with GPU-native frame handling for hardware encoders and `hwdownload` filter chain for CPU encoding. Add an HTML test page served at `/` using hls.js from CDN.

**Tech Stack:** Go, FFmpeg ddagrab (DXGI Desktop Duplication), hls.js (CDN)

---

## File Structure

```
Modified files:
├── internal/
│   ├── ffmpeg/
│   │   ├── command.go          # Update BuildArgs signature + ddagrab logic
│   │   ├── command_test.go     # Update existing tests + add ddagrab tests
│   │   ├── encoder.go          # Add ProbeDDAgrab function
│   │   └── encoder_test.go     # Add ProbeDDAgrab tests
│   └── hls/
│       ├── server.go           # Add playerHTML const + "/" route
│       └── server_test.go      # Add test for "/" route
└── cmd/
    └── vrshare/
        └── main.go             # Wire ddagrab probe, pass to BuildArgs, add warnings
```

---

### Task 1: Add ddagrab Probe Function

**Files:**
- Modify: `internal/ffmpeg/encoder.go`
- Modify: `internal/ffmpeg/encoder_test.go`

- [ ] **Step 1: Write failing test for ProbeDDAgrab**

Add to `internal/ffmpeg/encoder_test.go`:
```go
func TestProbeDDAgrab_Available(t *testing.T) {
	probe := func(output string) bool {
		return strings.Contains(output, "ddagrab")
	}
	if !probe("  D  ddagrab           Desktop Duplication API") {
		t.Error("should detect ddagrab in devices output")
	}
}

func TestProbeDDAgrab_NotAvailable(t *testing.T) {
	probe := func(output string) bool {
		return strings.Contains(output, "ddagrab")
	}
	if probe("  D  gdigrab           GDI API Windows frame grabber") {
		t.Error("should not detect ddagrab when not listed")
	}
}
```

Also add `"strings"` to the import block.

- [ ] **Step 2: Run tests to verify they pass**

These are self-contained tests that don't call any new function yet — they just verify the detection logic. They should pass.

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go test ./internal/ffmpeg/ -v -run TestProbeDDAgrab
```

- [ ] **Step 3: Implement ProbeDDAgrab**

Add to `internal/ffmpeg/encoder.go`:
```go
// ProbeDDAgrab checks if FFmpeg supports the ddagrab input device
// by running ffmpeg -devices and looking for "ddagrab" in the output.
func ProbeDDAgrab(ffmpegPath string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	out, err := exec.Command(ffmpegPath, "-hide_banner", "-devices").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "ddagrab")
}
```

Also add `"runtime"` to the import block.

- [ ] **Step 4: Run all ffmpeg tests**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go test ./internal/ffmpeg/ -v
```

Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ffmpeg/encoder.go internal/ffmpeg/encoder_test.go
git commit -m "feat: add ddagrab availability probe"
```

---

### Task 2: Update BuildArgs for ddagrab

**Files:**
- Modify: `internal/ffmpeg/command.go`
- Modify: `internal/ffmpeg/command_test.go`

- [ ] **Step 1: Write failing tests for ddagrab capture**

Add to `internal/ffmpeg/command_test.go`:
```go
func TestBuildArgs_DDAgrab_GPUEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true)

	assertContains(t, args, "-f", "ddagrab")
	assertContains(t, args, "-i", "0")
	assertContains(t, args, "-c:v", "h264_nvenc")
	assertNotContains(t, args, "-vf")
}

func TestBuildArgs_DDAgrab_CPUEncoder(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", true)

	assertContains(t, args, "-f", "ddagrab")
	assertContains(t, args, "-i", "0")
	assertContains(t, args, "-c:v", "libx264")
	assertContains(t, args, "-vf", "hwdownload,format=bgra,format=yuv420p")
}

func TestBuildArgs_DDAgrab_CPUEncoder_WithResolution(t *testing.T) {
	cfg := config.Default()
	cfg.Resolution = "1280x720"
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", true)

	assertContains(t, args, "-vf", "hwdownload,format=bgra,format=yuv420p,scale=1280:720")
}

func TestBuildArgs_DDAgrab_GPUEncoder_IgnoresResolution(t *testing.T) {
	cfg := config.Default()
	cfg.Resolution = "1280x720"
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true)

	assertNotContains(t, args, "-vf")
}

func TestBuildArgs_DDAgrab_MonitorIndex(t *testing.T) {
	cfg := config.Default()
	cfg.Monitor = 2
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true)

	assertContains(t, args, "-i", "2")
}

func TestBuildArgs_GdigrabFallback(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false)

	assertContains(t, args, "-f", "gdigrab")
	assertContains(t, args, "-i", "desktop")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go test ./internal/ffmpeg/ -v -run TestBuildArgs_DDAgrab
```

Expected: FAIL — `BuildArgs` takes 3 args, not 4.

- [ ] **Step 3: Update BuildArgs signature and implementation**

Replace `internal/ffmpeg/command.go` with:
```go
package ffmpeg

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vexedaa/vrshare/internal/config"
)

func isGPUEncoder(encoder string) bool {
	return encoder == "nvenc" || encoder == "qsv" || encoder == "amf"
}

func BuildArgs(cfg config.Config, resolvedEncoder string, segmentDir string, useDDAgrab bool) []string {
	args := []string{}

	// Input: platform-specific screen capture
	useDD := useDDAgrab && runtime.GOOS == "windows"

	if useDD {
		args = append(args, "-f", "ddagrab")
	} else {
		switch runtime.GOOS {
		case "linux":
			args = append(args, "-f", "x11grab")
		case "darwin":
			args = append(args, "-f", "avfoundation")
		default: // windows gdigrab fallback
			args = append(args, "-f", "gdigrab")
		}
	}

	args = append(args, "-framerate", fmt.Sprintf("%d", cfg.FPS))

	// Input source
	if useDD {
		args = append(args, "-i", fmt.Sprintf("%d", cfg.Monitor))
	} else {
		switch runtime.GOOS {
		case "linux":
			args = append(args, "-i", fmt.Sprintf(":%d.0", cfg.Monitor))
		case "darwin":
			args = append(args, "-i", fmt.Sprintf("%d", cfg.Monitor))
		default: // windows gdigrab
			if cfg.Monitor == 0 {
				args = append(args, "-i", "desktop")
			} else {
				args = append(args, "-i", "desktop")
				args = append(args, "-offset_x", "0", "-offset_y", "0")
			}
		}
	}

	// Encoder
	switch resolvedEncoder {
	case "nvenc":
		args = append(args, "-c:v", "h264_nvenc", "-preset", "p4", "-tune", "ll")
	case "qsv":
		args = append(args, "-c:v", "h264_qsv", "-preset", "veryfast")
	case "amf":
		args = append(args, "-c:v", "h264_amf", "-quality", "speed")
	default: // cpu
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency")
	}

	// Bitrate
	args = append(args, "-b:v", fmt.Sprintf("%dk", cfg.Bitrate))

	// Video filters: ddagrab frames need hwdownload for CPU encoder
	if useDD && !isGPUEncoder(resolvedEncoder) {
		vf := "hwdownload,format=bgra,format=yuv420p"
		if cfg.Resolution != "" {
			scaled := strings.Replace(cfg.Resolution, "x", ":", 1)
			vf += ",scale=" + scaled
		}
		args = append(args, "-vf", vf)
	} else if !useDD && cfg.Resolution != "" {
		// gdigrab or non-Windows: standard software scale
		scaled := strings.Replace(cfg.Resolution, "x", ":", 1)
		args = append(args, "-vf", fmt.Sprintf("scale=%s", scaled))
	}
	// GPU encoder + ddagrab + resolution: silently skip (caller warns)

	// Keyframe interval = 1 per second
	gop := fmt.Sprintf("%d", cfg.FPS)
	args = append(args, "-g", gop, "-keyint_min", gop)

	// HLS output
	args = append(args,
		"-f", "hls",
		"-hls_time", "1",
		"-hls_list_size", "3",
		"-hls_flags", "delete_segments+append_list",
		"-hls_segment_filename", filepath.Join(segmentDir, "segment_%d.ts"),
		filepath.Join(segmentDir, "stream.m3u8"),
	)

	return args
}
```

- [ ] **Step 4: Update existing tests to pass `false` for useDDAgrab**

In `internal/ffmpeg/command_test.go`, update all existing `BuildArgs` calls to add `, false` as the fourth argument:

- `TestBuildArgs_Defaults`: `BuildArgs(cfg, "cpu", "/tmp/vrshare", false)`
- `TestBuildArgs_CustomFPSAndBitrate`: `BuildArgs(cfg, "cpu", "/tmp/vrshare", false)`
- `TestBuildArgs_WithResolution`: `BuildArgs(cfg, "cpu", "/tmp/vrshare", false)`
- `TestBuildArgs_NVENCEncoder`: `BuildArgs(cfg, "nvenc", "/tmp/vrshare", false)`
- `TestBuildArgs_QSVEncoder`: `BuildArgs(cfg, "qsv", "/tmp/vrshare", false)`
- `TestBuildArgs_AMFEncoder`: `BuildArgs(cfg, "amf", "/tmp/vrshare", false)`
- `TestBuildArgs_CPUEncoder`: `BuildArgs(cfg, "cpu", "/tmp/vrshare", false)`
- `TestBuildArgs_OutputPaths`: `BuildArgs(cfg, "cpu", dir, false)`

- [ ] **Step 5: Run all tests**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go test ./internal/ffmpeg/ -v
```

Expected: All tests PASS (old and new).

- [ ] **Step 6: Commit**

```bash
git add internal/ffmpeg/command.go internal/ffmpeg/command_test.go
git commit -m "feat: switch Windows capture to ddagrab with gdigrab fallback"
```

---

### Task 3: Add Test Player Page

**Files:**
- Modify: `internal/hls/server.go`
- Modify: `internal/hls/server_test.go`

- [ ] **Step 1: Write failing test for player page**

Add to `internal/hls/server_test.go`:
```go
func TestServer_ServesPlayerPage(t *testing.T) {
	dir := t.TempDir()

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("expected text/html content type, got %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "hls.js") {
		t.Error("player page should reference hls.js")
	}
	if !strings.Contains(body, "stream.m3u8") {
		t.Error("player page should reference stream.m3u8")
	}
}
```

Also add `"strings"` to the import block in `server_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go test ./internal/hls/ -v -run TestServer_ServesPlayerPage
```

Expected: FAIL — `/` returns 404 (doesn't match `.m3u8` or `.ts` extension check).

- [ ] **Step 3: Add player HTML constant and `/` route to server.go**

Add the following constant at the top of `internal/hls/server.go` (after the import block, before the `Server` struct):
```go
const playerHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>VRShare</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#111;display:flex;align-items:center;justify-content:center;height:100vh}
video{max-width:100%;max-height:100vh}
</style>
</head>
<body>
<video id="v" controls autoplay muted></video>
<script src="https://cdn.jsdelivr.net/npm/hls.js@latest"></script>
<script>
var video=document.getElementById("v");
if(Hls.isSupported()){
  var hls=new Hls({liveSyncDurationCount:2,liveMaxLatencyDurationCount:4});
  hls.loadSource("/stream.m3u8");
  hls.attachMedia(video);
  hls.on(Hls.Events.MANIFEST_PARSED,function(){video.play()});
}else if(video.canPlayType("application/vnd.apple.mpegurl")){
  video.src="/stream.m3u8";
}
</script>
</body>
</html>`
```

Then update `ServeHTTP` in `server.go` — add the following block right after the CORS preflight check (after the `OPTIONS` handler, before the path cleaning):
```go
	// Serve test player page at root
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(playerHTML))
		return
	}
```

- [ ] **Step 4: Run all HLS tests**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go test ./internal/hls/ -v
```

Expected: All tests PASS (old and new).

- [ ] **Step 5: Commit**

```bash
git add internal/hls/server.go internal/hls/server_test.go
git commit -m "feat: add built-in test player page at /"
```

---

### Task 4: Wire ddagrab into main.go

**Files:**
- Modify: `cmd/vrshare/main.go`

- [ ] **Step 1: Update main.go**

In `cmd/vrshare/main.go`, add ddagrab probing and the resolution+GPU warning. Make these changes:

After the encoder detection block (after `log.Printf("Using encoder: %s", resolvedEncoder)`), add:
```go
	// Probe for ddagrab (DXGI Desktop Duplication) support
	useDDAgrab := ffmpeg.ProbeDDAgrab(ffmpegPath)
	if useDDAgrab {
		log.Println("Using ddagrab (DXGI Desktop Duplication) for capture")
	} else {
		log.Println("Using gdigrab for capture (ddagrab not available)")
	}

	// Warn if resolution scaling is set with GPU encoder + ddagrab
	if useDDAgrab && cfg.Resolution != "" && resolvedEncoder != "cpu" {
		log.Printf("Warning: --resolution is ignored with GPU encoder + ddagrab. Use native resolution or --encoder cpu.")
	}
```

Update the `BuildArgs` call (line 125) to pass `useDDAgrab`:
```go
	args := ffmpeg.BuildArgs(cfg, resolvedEncoder, segmentDir, useDDAgrab)
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go build ./cmd/vrshare
```

Expected: Compiles without errors.

- [ ] **Step 3: Run all tests**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go test ./... -v
```

Expected: All tests PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/vrshare/main.go
git commit -m "feat: wire ddagrab probe and resolution warning into main"
```

---

### Task 5: Smoke Test

**Files:** None (manual testing)

- [ ] **Step 1: Build and run with defaults**

```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go run ./cmd/vrshare
```

Expected: Log output shows "Using ddagrab (DXGI Desktop Duplication) for capture" and the stream starts. CPU/GPU usage should be significantly lower than before.

- [ ] **Step 2: Open test player in browser**

Open `http://localhost:8080/` in a browser.

Expected: Dark page with video player that auto-plays the desktop capture stream.

- [ ] **Step 3: Verify HLS endpoints still work**

```bash
curl -s -I http://localhost:8080/stream.m3u8
```

Expected: 200 OK with `Content-Type: application/vnd.apple.mpegurl`.

- [ ] **Step 4: Test with CPU encoder fallback**

```bash
go run ./cmd/vrshare --encoder cpu --resolution 1280x720
```

Expected: Stream works at 1280x720. Log shows ddagrab with CPU encoder. Open `http://localhost:8080/` to verify playback.

- [ ] **Step 5: Test resolution warning with GPU encoder**

```bash
go run ./cmd/vrshare --encoder nvenc --resolution 1280x720
```

Expected: Warning in log: "resolution is ignored with GPU encoder + ddagrab".

- [ ] **Step 6: Commit any fixes**

```bash
git add -A
git commit -m "chore: smoke test fixes (if any)"
```

Skip if no fixes needed.
