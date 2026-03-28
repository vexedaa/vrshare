package hls

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
  var hls=new Hls({liveSyncDurationCount:1,liveMaxLatencyDurationCount:2,lowLatencyMode:true,maxLiveSyncPlaybackRate:1.5,backBufferLength:0});
  hls.loadSource("/stream.m3u8");
  hls.attachMedia(video);
  hls.on(Hls.Events.MANIFEST_PARSED,function(){video.play()});
}else if(video.canPlayType("application/vnd.apple.mpegurl")){
  video.src="/stream.m3u8";
}
</script>
</body>
</html>`

// Server serves HLS segments, tracks active downloads, and provides
// a fragmented MP4 endpoint for players that don't support HLS.
type Server struct {
	dir          string
	port         int
	ffmpegPath   string
	blockTimeout time.Duration
	active       map[string]int       // segment name -> active reader count
	activeMu     sync.Mutex
	viewers      map[string]time.Time // IP -> last seen time
	viewersMu    sync.Mutex
}

func NewServer(dir string) *Server {
	return &Server{
		dir:          dir,
		blockTimeout: 5 * time.Second,
		active:       make(map[string]int),
		viewers:      make(map[string]time.Time),
	}
}

// SetMP4Support configures the server to offer /stream.mp4 by remuxing
// HLS segments via FFmpeg. Must be called before serving requests.
func (s *Server) SetMP4Support(ffmpegPath string, port int) {
	s.ffmpegPath = ffmpegPath
	s.port = port
}

// IsSegmentActive returns true if any viewer is currently downloading the segment.
func (s *Server) IsSegmentActive(name string) bool {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	return s.active[name] > 0
}

func (s *Server) trackStart(name string) {
	s.activeMu.Lock()
	s.active[name]++
	s.activeMu.Unlock()
}

func (s *Server) trackEnd(name string) {
	s.activeMu.Lock()
	s.active[name]--
	if s.active[name] <= 0 {
		delete(s.active, name)
	}
	s.activeMu.Unlock()
}

// ViewerCount returns the number of unique viewers seen in the last 5 seconds.
func (s *Server) ViewerCount() int {
	s.viewersMu.Lock()
	defer s.viewersMu.Unlock()
	cutoff := time.Now().Add(-5 * time.Second)
	count := 0
	for ip, lastSeen := range s.viewers {
		if lastSeen.After(cutoff) {
			count++
		} else {
			delete(s.viewers, ip)
		}
	}
	return count
}

func (s *Server) trackViewer(r *http.Request) {
	ip := r.RemoteAddr
	// Strip port
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	s.viewersMu.Lock()
	s.viewers[ip] = time.Now()
	s.viewersMu.Unlock()
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

	// Serve fragmented MP4 stream for players that don't support HLS
	if r.URL.Path == "/stream.mp4" {
		s.serveMP4(w, r)
		return
	}

	// Clean and validate path
	name := filepath.Clean(r.URL.Path)
	name = strings.TrimPrefix(name, "/")
	// On Windows filepath.Clean uses backslashes; normalise to forward slash
	// then re-clean so we work with the OS separator for file operations.
	name = filepath.FromSlash(name)

	// Only serve HLS-related files
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".m3u8" && ext != ".ts" && ext != ".m4s" && ext != ".mp4" {
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

	// LL-HLS blocking playlist: if _HLS_msn is present, wait until the
	// playlist contains the requested media sequence number.
	if ext == ".m3u8" {
		if msnStr := r.URL.Query().Get("_HLS_msn"); msnStr != "" {
			msn, _ := strconv.Atoi(msnStr)
			s.waitForMSN(fullPath, msn, s.blockTimeout)
		}
	}

	// Set content type and cache headers BEFORE calling ServeContent so they
	// are not overwritten by Go's content-sniffing logic.
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

	http.ServeContent(w, r, "", time.Time{}, f)
}

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
	}
	// The highest MSN present is baseMSN + segCount - 1
	return segCount > 0 && baseMSN+segCount-1 >= targetMSN
}

// serveMP4 spawns an FFmpeg process that reads the local HLS playlist and
// remuxes it into a fragmented MP4 stream, piped to the HTTP response.
// No re-encoding — just copies packets. Killed when the viewer disconnects.
func (s *Server) serveMP4(w http.ResponseWriter, r *http.Request) {
	if s.ffmpegPath == "" || s.port == 0 {
		http.Error(w, "MP4 streaming not configured", http.StatusServiceUnavailable)
		return
	}

	hlsURL := fmt.Sprintf("http://localhost:%d/stream.m3u8", s.port)

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
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "failed to create pipe", http.StatusInternalServerError)
		return
	}

	if err := cmd.Start(); err != nil {
		http.Error(w, "failed to start remuxer", http.StatusInternalServerError)
		return
	}

	log.Printf("MP4 viewer connected from %s", r.RemoteAddr)

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Transfer-Encoding", "chunked")

	// Stream FFmpeg's stdout to the HTTP response.
	// When the viewer disconnects, r.Context() is cancelled,
	// which kills the FFmpeg process via CommandContext.
	buf := make([]byte, 32*1024)
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				break
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}

	cmd.Wait()
	log.Printf("MP4 viewer disconnected from %s", r.RemoteAddr)
}
