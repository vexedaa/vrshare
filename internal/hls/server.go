package hls

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Server struct {
	dir string
}

func NewServer(dir string) *Server {
	return &Server{dir: dir}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS headers on all responses
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Range")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Clean and validate path
	name := filepath.Clean(r.URL.Path)
	name = strings.TrimPrefix(name, "/")
	// On Windows filepath.Clean uses backslashes; normalise to forward slash
	// then re-clean so we work with the OS separator for file operations.
	name = filepath.FromSlash(name)

	// Only serve .m3u8 and .ts files
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".m3u8" && ext != ".ts" {
		http.NotFound(w, r)
		return
	}

	// Prevent path traversal: the cleaned name must not contain ".."
	// and the resolved full path must remain inside s.dir.
	fullPath := filepath.Join(s.dir, name)
	if !strings.HasPrefix(filepath.Clean(fullPath)+string(filepath.Separator),
		filepath.Clean(s.dir)+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}

	// Check file exists and is a regular file
	fi, err := os.Stat(fullPath)
	if err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return
	}

	// Open the file so we can serve it with http.ServeContent,
	// which respects Range requests without overriding our headers.
	f, err := os.Open(fullPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	// Set content type and cache headers BEFORE calling ServeContent so they
	// are not overwritten by Go's content-sniffing logic.
	switch ext {
	case ".m3u8":
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
	case ".ts":
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Cache-Control", "max-age=3600")
	}

	http.ServeContent(w, r, "", time.Time{}, f)
}
