package hls

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServer_ServesM3U8WithCorrectHeaders(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte("#EXTM3U\n"), 0644)

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/stream.m3u8", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
		t.Errorf("expected m3u8 content type, got %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected no-cache for m3u8, got %q", cc)
	}
	if cors := w.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("expected CORS *, got %q", cors)
	}
}

func TestServer_ServesTSWithCorrectHeaders(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "segment_0.ts"), []byte("fake-ts-data"), 0644)

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/segment_0.ts", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Errorf("expected video/mp2t, got %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "max-age=3600" {
		t.Errorf("expected max-age=3600 for .ts, got %q", cc)
	}
	if cors := w.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("expected CORS *, got %q", cors)
	}
}

func TestServer_Returns404ForMissingFile(t *testing.T) {
	dir := t.TempDir()

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/nonexistent.m3u8", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestServer_BlocksPathTraversal(t *testing.T) {
	dir := t.TempDir()

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/../../../etc/passwd", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("path traversal should not return 200")
	}
}

func TestServer_CORSPreflight(t *testing.T) {
	dir := t.TempDir()

	srv := NewServer(dir)
	req := httptest.NewRequest("OPTIONS", "/stream.m3u8", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS, got %d", w.Code)
	}
	if cors := w.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("expected CORS *, got %q", cors)
	}
}

func TestServer_OnlyServesAllowedExtensions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("secrets"), 0644)

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/secret.txt", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("should reject non-HLS files, got %d", w.Code)
	}
}

func TestServer_ViewerCount(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte("#EXTM3U\n"), 0644)
	srv := NewServer(dir)

	if srv.ViewerCount() != 0 {
		t.Fatalf("initial viewer count should be 0, got %d", srv.ViewerCount())
	}

	req := httptest.NewRequest("GET", "/stream.m3u8", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if srv.ViewerCount() != 1 {
		t.Fatalf("viewer count should be 1 after playlist request, got %d", srv.ViewerCount())
	}
}

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
	// Playlist already has msn=0 with 1 segment
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXTINF:0.5,\nsegment_0.m4s\n"
	os.WriteFile(filepath.Join(dir, "stream.m3u8"), []byte(playlist), 0644)

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/stream.m3u8?_HLS_msn=0", nil)
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
	srv.blockTimeout = 200 * time.Millisecond
	req := httptest.NewRequest("GET", "/stream.m3u8?_HLS_msn=99", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should still return 200 with whatever playlist is current
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
		req := httptest.NewRequest("GET", "/stream.m3u8?_HLS_msn=1", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		done <- w.Code
	}()

	// Simulate FFmpeg writing an updated playlist after 100ms
	time.Sleep(100 * time.Millisecond)
	updated := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXTINF:0.5,\nsegment_0.m4s\n" +
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
