package hls

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
