package hls

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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

	// Serve test player page at root
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(playerHTML))
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
