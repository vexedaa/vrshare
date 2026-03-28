# Low-Latency HLS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce stream latency from ~5s to ~1-2s through a tunnel by switching to LL-HLS with partial segments.

**Architecture:** FFmpeg outputs fMP4 partial segments with `low_latency` flag. The HLS server gains blocking playlist support — when HLS.js requests a specific media sequence/part, the server holds the response until that part appears on disk. HLS.js already supports LL-HLS natively via `lowLatencyMode`.

**Tech Stack:** Go (net/http), FFmpeg LL-HLS flags, HLS.js player

---

### Task 1: Update FFmpeg args for LL-HLS fMP4 output

**Files:**
- Modify: `internal/ffmpeg/command.go:93-104`
- Modify: `internal/ffmpeg/command_test.go`

- [ ] **Step 1: Update existing tests to expect new values**

In `internal/ffmpeg/command_test.go`, update `TestBuildArgs_Defaults` to expect the new LL-HLS args:

```go
func TestBuildArgs_Defaults(t *testing.T) {
	cfg := config.Default()
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false)

	assertContains(t, args, "-f", "gdigrab")
	assertContains(t, args, "-framerate", "30")
	assertContains(t, args, "-i", "desktop")
	assertContains(t, args, "-c:v", "libx264")
	assertContains(t, args, "-b:v", "4000k")
	assertContains(t, args, "-g", "15")
	assertContains(t, args, "-f", "hls")
	assertContains(t, args, "-hls_time", "0.5")
	assertContains(t, args, "-hls_list_size", "2")
	assertContains(t, args, "-hls_segment_type", "fmp4")
	assertContains(t, args, "-hls_fmp4_init_filename", "init.mp4")
	assertContains(t, args, "-hls_flags", "append_list+delete_segments+low_latency")
	assertContains(t, args, "-flush_packets", "1")
	assertNotContains(t, args, "-vf")
}
```

Update `TestBuildArgs_CustomFPSAndBitrate` — GOP is now FPS/2:

```go
func TestBuildArgs_CustomFPSAndBitrate(t *testing.T) {
	cfg := config.Default()
	cfg.FPS = 60
	cfg.Bitrate = 6000
	args := BuildArgs(cfg, "cpu", "/tmp/vrshare", false)

	assertContains(t, args, "-framerate", "60")
	assertContains(t, args, "-b:v", "6000k")
	assertContains(t, args, "-g", "30")
}
```

Update `TestBuildArgs_OutputPaths` — segments are now `.m4s`:

```go
func TestBuildArgs_OutputPaths(t *testing.T) {
	cfg := config.Default()
	dir := t.TempDir()
	args := BuildArgs(cfg, "cpu", dir, false)

	expectedSeg := filepath.Join(dir, "segment_%d.m4s")
	assertContains(t, args, "-hls_segment_filename", expectedSeg)
	expectedPlaylist := filepath.Join(dir, "stream.m3u8")
	if args[len(args)-1] != expectedPlaylist {
		t.Errorf("last arg should be playlist path, got %q", args[len(args)-1])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOOS=windows go test ./internal/ffmpeg/ -run "TestBuildArgs_Defaults|TestBuildArgs_CustomFPS|TestBuildArgs_OutputPaths" -v`

Expected: FAIL — tests expect new values but code still produces old ones.

- [ ] **Step 3: Update BuildArgs in command.go**

Replace the HLS output section at the end of `BuildArgs` (lines 93-106) with:

```go
	gop := fmt.Sprintf("%d", cfg.FPS/2)
	args = append(args, "-g", gop, "-keyint_min", gop)

	args = append(args,
		"-f", "hls",
		"-hls_time", "0.5",
		"-hls_list_size", "2",
		"-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", "init.mp4",
		"-hls_flags", "append_list+delete_segments+low_latency",
		"-flush_packets", "1",
		"-hls_segment_filename", filepath.Join(segmentDir, "segment_%d.m4s"),
		filepath.Join(segmentDir, "stream.m3u8"),
	)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOOS=windows go test ./internal/ffmpeg/ -v`

Expected: All tests pass. Note: `TestBuildArgs_DDAgrab_CustomFPS` expects `-g "60"` — this must now be `"30"` (FPS/2). Update that assertion too if needed.

- [ ] **Step 5: Commit**

```
git add internal/ffmpeg/command.go internal/ffmpeg/command_test.go
git commit -m "feat: switch to LL-HLS fMP4 output with 0.5s segments"
```

---

### Task 2: Update janitor to clean fMP4 segments

**Files:**
- Modify: `internal/hls/janitor.go`
- Modify: `internal/hls/janitor_test.go`

- [ ] **Step 1: Write a test for fMP4 segment cleanup**

Add to `internal/hls/janitor_test.go`:

```go
func TestCleanOldSegments_RemovesFMP4Files(t *testing.T) {
	dir := t.TempDir()

	// Create init.mp4 and m4s segments
	os.WriteFile(filepath.Join(dir, "init.mp4"), []byte("init"), 0644)
	for i := 0; i <= 5; i++ {
		name := filepath.Join(dir, fmt.Sprintf("segment_%d.m4s", i))
		os.WriteFile(name, []byte("data"), 0644)
	}

	// Playlist references segments 3-5
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n" +
		"#EXT-X-MAP:URI=\"init.mp4\"\n" +
		"#EXTINF:0.5,\nsegment_3.m4s\n" +
		"#EXTINF:0.5,\nsegment_4.m4s\n" +
		"#EXTINF:0.5,\nsegment_5.m4s\n"
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte(playlist), 0644)

	removed, err := CleanOldSegments(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if removed != 3 {
		t.Errorf("expected 3 removed, got %d", removed)
	}

	// init.mp4 should NOT be removed
	if _, err := os.Stat(filepath.Join(dir, "init.mp4")); os.IsNotExist(err) {
		t.Error("init.mp4 should not be removed")
	}

	// Referenced segments should still exist
	for i := 3; i <= 5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("segment_%d.m4s", i))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("segment_%d.m4s should still exist", i)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOOS=windows go test ./internal/hls/ -run TestCleanOldSegments_RemovesFMP4 -v`

Expected: FAIL — janitor currently only handles `.ts` files.

- [ ] **Step 3: Update janitor to handle both .ts and .m4s**

In `internal/hls/janitor.go`, update `CleanOldSegments` to also process `.m4s` files:

```go
func CleanOldSegments(dir string, srv *Server) (int, error) {
	playlistPath := filepath.Join(dir, "stream.m3u8")

	referenced, err := parsePlaylistSegments(playlistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	if len(referenced) == 0 {
		return 0, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".ts" && ext != ".m4s" {
			continue
		}
		if referenced[entry.Name()] {
			continue
		}
		if srv != nil && srv.IsSegmentActive(entry.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err == nil {
			removed++
		}
	}

	return removed, nil
}
```

Update `parsePlaylistSegments` to also match `.m4s` files:

```go
func parsePlaylistSegments(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	segments := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasSuffix(line, ".ts") || strings.HasSuffix(line, ".m4s") {
			segments[line] = true
		}
		// Parse EXT-X-MAP URI for init segment
		if strings.HasPrefix(line, "#EXT-X-MAP:") {
			if start := strings.Index(line, "URI=\""); start != -1 {
				start += 5
				if end := strings.Index(line[start:], "\""); end != -1 {
					segments[line[start:start+end]] = true
				}
			}
		}
		// Parse EXT-X-PART URI for partial segments
		if strings.HasPrefix(line, "#EXT-X-PART:") {
			if start := strings.Index(line, "URI=\""); start != -1 {
				start += 5
				if end := strings.Index(line[start:], "\""); end != -1 {
					segments[line[start:start+end]] = true
				}
			}
		}
	}
	return segments, scanner.Err()
}
```

- [ ] **Step 4: Run all janitor tests**

Run: `GOOS=windows go test ./internal/hls/ -run TestCleanOldSegments -v`

Expected: All pass, including existing `.ts` tests and new `.m4s` test.

- [ ] **Step 5: Commit**

```
git add internal/hls/janitor.go internal/hls/janitor_test.go
git commit -m "feat: janitor cleans fMP4 segments and parses LL-HLS playlist tags"
```

---

### Task 3: Update HLS server to serve fMP4 files and support blocking playlist

**Files:**
- Modify: `internal/hls/server.go`
- Modify: `internal/hls/server_test.go`

- [ ] **Step 1: Write tests for fMP4 serving and blocking playlist**

Add to `internal/hls/server_test.go`:

```go
func TestServer_ServesM4SWithCorrectHeaders(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "segment_0.m4s"), []byte("fake-fmp4-data"), 0644)

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/segment_0.m4s", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "video/mp4" {
		t.Errorf("expected video/mp4, got %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "max-age=3600" {
		t.Errorf("expected max-age=3600, got %q", cc)
	}
}

func TestServer_ServesInitMP4(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "init.mp4"), []byte("fake-init"), 0644)

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/init.mp4", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "video/mp4" {
		t.Errorf("expected video/mp4, got %q", ct)
	}
}

func TestServer_BlockingPlaylist_ImmediateReturn(t *testing.T) {
	dir := t.TempDir()
	// Playlist already has msn=2 part=0
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXT-X-PART:DURATION=0.25,URI=\"segment_2.0.m4s\"\n" +
		"#EXTINF:0.5,\nsegment_2.m4s\n"
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte(playlist), 0644)

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/stream.m3u8?_HLS_msn=2&_HLS_part=0", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestServer_BlockingPlaylist_TimeoutReturns200(t *testing.T) {
	dir := t.TempDir()
	// Playlist has msn=0 but client wants msn=99
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXTINF:0.5,\nsegment_0.m4s\n"
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte(playlist), 0644)

	srv := NewServer(dir)
	// Use a short timeout override for testing
	srv.blockTimeout = 200 * time.Millisecond
	req := httptest.NewRequest("GET", "/stream.m3u8?_HLS_msn=99&_HLS_part=0", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should still return 200 with whatever playlist is current (not 503)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestServer_BlockingPlaylist_WaitsForUpdate(t *testing.T) {
	dir := t.TempDir()
	// Playlist has msn=0
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXTINF:0.5,\nsegment_0.m4s\n"
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte(playlist), 0644)

	srv := NewServer(dir)
	srv.blockTimeout = 2 * time.Second

	done := make(chan int)
	go func() {
		req := httptest.NewRequest("GET", "/stream.m3u8?_HLS_msn=1&_HLS_part=0", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		done <- w.Code
	}()

	// Simulate FFmpeg writing an updated playlist after 100ms
	time.Sleep(100 * time.Millisecond)
	updated := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXTINF:0.5,\nsegment_0.m4s\n" +
		"#EXT-X-PART:DURATION=0.25,URI=\"segment_1.0.m4s\"\n" +
		"#EXTINF:0.5,\nsegment_1.m4s\n"
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte(updated), 0644)

	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocking playlist timed out")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOOS=windows go test ./internal/hls/ -run "TestServer_ServesM4S|TestServer_ServesInitMP4|TestServer_BlockingPlaylist" -v`

Expected: FAIL — `.m4s`/`.mp4` not served, blocking not implemented, `blockTimeout` field doesn't exist.

- [ ] **Step 3: Add blockTimeout field and update allowed extensions**

In `internal/hls/server.go`, update the `Server` struct to add `blockTimeout`:

```go
type Server struct {
	dir          string
	port         int
	ffmpegPath   string
	blockTimeout time.Duration
	active       map[string]int
	activeMu     sync.Mutex
	viewers      map[string]time.Time
	viewersMu    sync.Mutex
}
```

Update `NewServer`:

```go
func NewServer(dir string) *Server {
	return &Server{
		dir:          dir,
		blockTimeout: 5 * time.Second,
		active:       make(map[string]int),
		viewers:      make(map[string]time.Time),
	}
}
```

Update the extension whitelist in `ServeHTTP` to allow `.m4s` and `.mp4`:

```go
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".m3u8" && ext != ".ts" && ext != ".m4s" && ext != ".mp4" {
		http.NotFound(w, r)
		return
	}
```

Update content-type handling — add `.m4s` and `.mp4` cases:

```go
	switch ext {
	case ".m3u8":
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		s.trackViewer(r)
	case ".ts":
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Cache-Control", "max-age=3600")
		s.trackStart(name)
		defer s.trackEnd(name)
	case ".m4s", ".mp4":
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "max-age=3600")
		s.trackStart(name)
		defer s.trackEnd(name)
	}
```

- [ ] **Step 4: Implement blocking playlist logic**

Add these imports to `server.go` if not present: `"strconv"`, `"time"`.

Add the blocking playlist handler. In `ServeHTTP`, replace the `.m3u8` handling in the switch with blocking-aware logic. Before the `switch ext` block, insert:

```go
	// LL-HLS blocking playlist: if _HLS_msn is present, wait until the
	// playlist contains the requested media sequence number.
	if ext == ".m3u8" {
		if msnStr := r.URL.Query().Get("_HLS_msn"); msnStr != "" {
			msn, _ := strconv.Atoi(msnStr)
			s.waitForMSN(fullPath, msn, s.blockTimeout)
		}
	}
```

Add the `waitForMSN` method:

```go
// waitForMSN polls the playlist file until it contains the given media
// sequence number or the timeout expires. It checks every 50ms.
func (s *Server) waitForMSN(playlistPath string, targetMSN int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.playlistContainsMSN(playlistPath, targetMSN) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// playlistContainsMSN returns true if the playlist file references enough
// segments to cover the target media sequence number. It counts segment
// entries (lines ending in .m4s or .ts) from the EXT-X-MEDIA-SEQUENCE base.
func (s *Server) playlistContainsMSN(path string, targetMSN int) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	baseMSN := 0
	segCount := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			baseMSN, _ = strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"))
		}
		if strings.HasSuffix(line, ".m4s") || strings.HasSuffix(line, ".ts") {
			segCount++
		}
		// Also count partial segments toward the target
		if strings.HasPrefix(line, "#EXT-X-PART:") {
			segCount++
		}
	}
	// The highest MSN present is baseMSN + segCount - 1
	return segCount > 0 && baseMSN+segCount-1 >= targetMSN
}
```

Add `"bufio"` and `"strconv"` to the imports.

- [ ] **Step 5: Run all HLS server tests**

Run: `GOOS=windows go test ./internal/hls/ -v`

Expected: All pass.

- [ ] **Step 6: Commit**

```
git add internal/hls/server.go internal/hls/server_test.go
git commit -m "feat: serve fMP4 segments and support LL-HLS blocking playlist"
```

---

### Task 4: Update HLS.js player config for LL-HLS

**Files:**
- Modify: `internal/hls/server.go` (playerHTML constant)
- Modify: `internal/hls/server_test.go`

- [ ] **Step 1: Update the player test**

Add to `internal/hls/server_test.go`:

```go
func TestServer_PlayerPage_LLHLSConfig(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "maxLiveSyncPlaybackRate") {
		t.Error("player should configure maxLiveSyncPlaybackRate for catchup")
	}
	if !strings.Contains(body, "backBufferLength") {
		t.Error("player should configure backBufferLength")
	}
	if !strings.Contains(body, "liveMaxLatencyDurationCount") {
		t.Error("player should configure liveMaxLatencyDurationCount")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOOS=windows go test ./internal/hls/ -run TestServer_PlayerPage_LLHLSConfig -v`

Expected: FAIL — current playerHTML doesn't include the new config keys.

- [ ] **Step 3: Update playerHTML**

Replace the HLS.js config in the `playerHTML` constant:

```javascript
if(Hls.isSupported()){
  var hls=new Hls({liveSyncDurationCount:1,liveMaxLatencyDurationCount:2,lowLatencyMode:true,maxLiveSyncPlaybackRate:1.5,backBufferLength:0});
  hls.loadSource("/stream.m3u8");
  hls.attachMedia(video);
  hls.on(Hls.Events.MANIFEST_PARSED,function(){video.play()});
}
```

- [ ] **Step 4: Run all HLS tests**

Run: `GOOS=windows go test ./internal/hls/ -v`

Expected: All pass.

- [ ] **Step 5: Commit**

```
git add internal/hls/server.go internal/hls/server_test.go
git commit -m "feat: tune HLS.js player for LL-HLS low-latency playback"
```

---

### Task 5: Reduce janitor sweep interval

**Files:**
- Modify: `internal/server/server.go:131`

- [ ] **Step 1: Update janitor interval from 5s to 2s**

In `internal/server/server.go`, change line 131:

```go
	go hls.RunJanitor(s.srvCtx, segDir, s.hlsSrv, 2*time.Second)
```

- [ ] **Step 2: Build to verify compilation**

Run: `GOOS=windows go build ./...`

Expected: Clean build.

- [ ] **Step 3: Commit**

```
git add internal/server/server.go
git commit -m "feat: reduce janitor sweep interval to 2s for shorter segments"
```

---

### Task 6: Update MP4 remuxer for fMP4 compatibility

**Files:**
- Modify: `internal/hls/server.go` (serveMP4 method)

- [ ] **Step 1: Update serveMP4 FFmpeg command**

The fMP4 segments already use the correct AAC bitstream format, so the `aac_adtstoasc` filter is no longer needed. It may cause errors with fMP4 input. Remove it:

In `serveMP4`, change the FFmpeg command from:

```go
	cmd := exec.CommandContext(r.Context(), s.ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-fflags", "nobuffer",
		"-live_start_index", "-1",
		"-i", hlsURL,
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-reset_timestamps", "1",
		"-f", "mp4",
		"pipe:1",
	)
```

To:

```go
	cmd := exec.CommandContext(r.Context(), s.ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-fflags", "nobuffer",
		"-live_start_index", "-1",
		"-i", hlsURL,
		"-c", "copy",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-reset_timestamps", "1",
		"-f", "mp4",
		"pipe:1",
	)
```

- [ ] **Step 2: Build to verify compilation**

Run: `GOOS=windows go build ./...`

Expected: Clean build.

- [ ] **Step 3: Commit**

```
git add internal/hls/server.go
git commit -m "fix: remove aac_adtstoasc filter for fMP4 segment compatibility"
```

---

### Task 7: Full integration build + test

- [ ] **Step 1: Run all tests**

Run: `GOOS=windows go test ./... -v`

Expected: All pass.

- [ ] **Step 2: Run go vet**

Run: `GOOS=windows go vet ./...`

Expected: No new warnings (pre-existing unsafe.Pointer warnings in WASAPI code are OK).

- [ ] **Step 3: Build the binary**

Run: `GOOS=windows go build -o vrshare.exe ./cmd/vrshare/`

Expected: Clean build, binary produced.
