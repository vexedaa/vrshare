package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vexedaa/vrshare/internal/config"
	"github.com/vexedaa/vrshare/internal/ffmpeg"
	"github.com/vexedaa/vrshare/internal/hls"
	"github.com/vexedaa/vrshare/internal/tunnel"
)

// maxLogEntries caps the in-memory event-log buffer. The full session history
// is always written to the session log file; this buffer is only the live tail
// shown in the UI. Without a cap it grew unbounded (~2 lines/sec from FFmpeg's
// per-segment logging) and was copied and emitted to the frontend every second,
// turning into a steadily rising CPU and memory cost over a long session.
const maxLogEntries = 500

// Server orchestrates the streaming pipeline: FFmpeg, HLS, audio, and tunnel.
type Server struct {
	cfg        config.Config
	mu         sync.Mutex
	status     string
	startTime  time.Time
	streamURL  string
	localURL   string // local-network URL; the fallback when no tunnel is up
	errMsg     string
	tunnelErr  string // last tunnel failure, surfaced to the UI (empty = healthy)

	// Server-level context (HLS, janitor, tunnel, audio)
	srvCancel  context.CancelFunc
	srvCtx     context.Context

	// FFmpeg-level context (can be restarted independently)
	ffmpegCancel context.CancelFunc
	ffmpegDone   chan struct{}

	hlsSrv     *hls.Server
	httpSrv    *http.Server
	tun        *tunnel.Tunnel
	tunnelMu   sync.Mutex // serializes tunnel start/restart/stop (tunnel.Start blocks for seconds)
	stats      *StatsParser
	ffmpegPath string
	useDDAgrab bool
	encoder    string
	segDir     string
	audioPipe  *os.File
	audio      *audioCapturer // nil when audio is disabled
	logEntries []LogEntry
	logSeq     uint64 // monotonic count of log lines ever emitted (for change detection)
	logMu      sync.Mutex
	logFile    *os.File
}

// LogEntry is a timestamped log message.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

// New creates a new Server with the given config.
func New(cfg config.Config) *Server {
	return &Server{
		cfg:    cfg,
		status: "idle",
	}
}

// Start starts the streaming pipeline.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.status == "streaming" || s.status == "starting" {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.status = "starting"
	s.errMsg = ""
	s.logEntries = nil
	s.mu.Unlock()

	// Open persistent log file for this session
	s.openSessionLog()

	// Kill orphaned FFmpeg/tunnel processes from previous crashed sessions.
	// This is critical because ddagrab (DXGI Desktop Duplication) only allows
	// one capture per display — a zombie FFmpeg will block new captures.
	killZombies(s.cfg.Port)

	s.log("Starting server...")

	// Find FFmpeg
	ffmpegPath, err := ffmpeg.FindFFmpeg()
	if err != nil {
		s.setError("FFmpeg not found: " + err.Error())
		return err
	}
	s.ffmpegPath = ffmpegPath
	s.log("FFmpeg found: " + ffmpegPath)

	// Probe encoder
	s.resolveEncoder()
	s.useDDAgrab = ffmpeg.ProbeDDAgrab(ffmpegPath)
	s.log(fmt.Sprintf("Encoder: %s, DDAgrab: %v", s.encoder, s.useDDAgrab))

	// Create temp segment directory
	segDir, err := os.MkdirTemp("", "vrshare-segments-*")
	if err != nil {
		s.setError("Failed to create segment dir: " + err.Error())
		return err
	}
	s.segDir = segDir

	// Sweep segment dirs left behind by sessions that crashed or were killed
	// before Stop() could remove their own dir. The age gate avoids deleting a
	// dir that a concurrent instance is actively writing (it touches it every
	// second), while still clearing genuine leftovers.
	if n := cleanupStaleSegmentDirs(os.TempDir(), segDir, 10*time.Minute); n > 0 {
		s.log(fmt.Sprintf("Cleaned %d stale segment dir(s)", n))
	}

	// Start HLS server — bind the port first so we fail fast if it's in use
	s.hlsSrv = hls.NewServer(segDir)
	s.hlsSrv.SetMP4Support(ffmpegPath, s.cfg.Port)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Port))
	if err != nil {
		s.setError(fmt.Sprintf("Port %d is already in use (is another instance running?)", s.cfg.Port))
		return fmt.Errorf("port %d already in use", s.cfg.Port)
	}
	s.httpSrv = &http.Server{Handler: s.hlsSrv}

	go func() {
		if err := s.httpSrv.Serve(ln); err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Build stream URL
	ip := getOutboundIP()
	s.mu.Lock()
	s.localURL = fmt.Sprintf("http://%s:%d/stream.m3u8", ip, s.cfg.Port)
	s.streamURL = s.localURL
	s.tunnelErr = ""
	s.mu.Unlock()
	s.log("Stream URL: " + s.localURL)

	// Server-level context for long-lived services
	s.srvCtx, s.srvCancel = context.WithCancel(ctx)

	// Janitor sweeps every 2s but only deletes segments older than 30s.
	// FFmpeg's delete_segments + hls_delete_threshold handles the normal
	// rolling cleanup; the janitor is just a safety net for stragglers.
	// Without an age gate, the janitor races viewers and deletes segments
	// while they are still being fetched.
	go hls.RunJanitor(s.srvCtx, segDir, s.hlsSrv, 2*time.Second, 30*time.Second)

	// Create audio pipe if enabled (FFmpeg needs the read-end at startup)
	var ac *audioCapturer
	if s.cfg.Audio {
		s.log("Audio capture enabled")
		r, w, err := os.Pipe()
		if err != nil {
			s.srvCancel()
			s.httpSrv.Shutdown(context.Background())
			s.setError("Failed to create audio pipe: " + err.Error())
			return err
		}
		s.audioPipe = r
		ac = newAudioCapturer(s.srvCtx, w, s.cfg.AudioDevice)
	}
	s.audio = ac

	// Start the tunnel if configured. A failure here is non-fatal: the stream
	// still works over the local network and the failure is surfaced (TunnelError)
	// so the UI can offer a retry.
	s.startTunnel()

	// Start the audio capturer right before FFmpeg. Audio must reach FFmpeg
	// continuously from startup: FFmpeg stalls its entire pipeline — encoding
	// no frames and writing no HLS segments — for as long as its mapped pipe:0
	// audio input is starved. The AsyncWriter therefore forwards every chunk
	// immediately (no first-frame gating, which would deadlock: no audio -> no
	// frame -> no signal -> no audio).
	if ac != nil {
		go ac.start(s.srvCtx)
	}

	if err := s.startFFmpeg(); err != nil {
		s.srvCancel()
		s.httpSrv.Shutdown(context.Background())
		return err
	}

	s.mu.Lock()
	s.status = "streaming"
	s.startTime = time.Now()
	s.mu.Unlock()
	s.log("Stream started")

	return nil
}

// resolveEncoder picks the encoder for the configured choice, test-encoding to
// confirm hardware encoders work. If an explicitly chosen encoder doesn't work
// on this machine, it says so and uses the best one that does.
func (s *Server) resolveEncoder() {
	configured := string(s.cfg.Encoder)
	probe := ffmpeg.ProbeFFmpegEncoder(s.ffmpegPath)
	s.encoder = ffmpeg.ResolveEncoder(configured, probe)
	if configured != "auto" && configured != s.encoder {
		s.log(fmt.Sprintf("Encoder %s is not usable on this system — using %s instead", configured, s.encoder))
	}
}

// discardStaleAudio drops audio that piled up while no FFmpeg was reading the
// pipe (between a crash or restart and the next launch). Feeding that backlog
// to the new process would leave audio that far behind video for the rest of
// the session — up to the full ~2.5s buffer after a crash-loop fallback.
func (s *Server) discardStaleAudio() {
	if s.audio == nil {
		return
	}
	if n := s.audio.discardStale(); n > 0 {
		s.log(fmt.Sprintf("Audio: discarded %d stale chunks buffered while FFmpeg was restarting", n))
	}
}

// startFFmpeg launches the FFmpeg process with current config.
// If FFmpeg crashes repeatedly, it falls back to safer settings
// (CPU encoder, gdigrab) before giving up entirely.
func (s *Server) startFFmpeg() error {
	encoder := s.encoder
	useDDAgrab := s.useDDAgrab

	s.stats = NewStatsParser(os.Stderr)
	s.stats.LogFunc = func(line string) { s.log("FFmpeg: " + line) }

	ffCtx, ffCancel := context.WithCancel(s.srvCtx)
	s.ffmpegCancel = ffCancel
	s.ffmpegDone = make(chan struct{})

	go func() {
		defer close(s.ffmpegDone)

		// Try up to 3 configurations: original → cpu encoder → cpu+gdigrab
		configs := []struct {
			encoder    string
			useDDAgrab bool
			label      string
		}{
			{encoder, useDDAgrab, ""},
			{"cpu", useDDAgrab, "falling back to CPU encoder"},
			{"cpu", false, "falling back to CPU encoder + gdigrab"},
		}

		for i, c := range configs {
			if ffCtx.Err() != nil {
				return
			}
			// Skip redundant fallbacks (e.g., already using cpu)
			if i > 0 && c.encoder == configs[i-1].encoder && c.useDDAgrab == configs[i-1].useDDAgrab {
				continue
			}
			if c.label != "" {
				s.log("FFmpeg: " + c.label)
			}

			args := ffmpeg.BuildArgs(s.cfg, c.encoder, s.segDir, c.useDDAgrab)
			mgr := ffmpeg.NewManager(s.ffmpegPath, s.segDir)
			mgr.StderrWriter = s.stats
			mgr.LogFunc = func(msg string) { s.log("FFmpeg: " + msg) }
			mgr.MaxRestarts = 2 // fewer retries per config before falling back
			mgr.BeforeStart = s.discardStaleAudio

			err := mgr.Run(ffCtx, args, s.audioPipe)
			if ffCtx.Err() != nil {
				return
			}
			if err == nil {
				return
			}
			s.log(fmt.Sprintf("FFmpeg: config %d failed: %v", i+1, err))
		}

		s.failStream("FFmpeg failed with all encoder configurations")
	}()

	return nil
}

// RestartCapture stops only FFmpeg and relaunches it with current config.
// The HLS server, tunnel, and audio capturer stay running.
func (s *Server) RestartCapture() error {
	s.mu.Lock()
	if s.status != "streaming" {
		s.mu.Unlock()
		return fmt.Errorf("not streaming")
	}
	s.mu.Unlock()

	s.log("Restarting capture...")

	// Stop FFmpeg only
	if s.ffmpegCancel != nil {
		s.ffmpegCancel()
	}
	if s.ffmpegDone != nil {
		<-s.ffmpegDone
	}

	// Re-probe encoder in case config changed
	s.resolveEncoder()

	// Apply an audio source change. The capturer keeps running (it owns the
	// pipe FFmpeg reads), so it just switches source in place.
	if s.audio != nil {
		s.audio.setDevice(s.cfg.AudioDevice)
	}

	// Relaunch FFmpeg. The AsyncWriter forwards audio unconditionally, so the
	// new process begins receiving PCM as soon as it opens the pipe.
	if err := s.startFFmpeg(); err != nil {
		s.setError("Failed to restart capture: " + err.Error())
		return err
	}

	s.log("Capture restarted")
	return nil
}

// startTunnel starts the configured tunnel (if any) and points streamURL at it.
// Serialized by tunnelMu so Retry clicks and Stop can't race. tunnel.Start blocks
// for seconds, so s.mu is taken only for the quick field swaps — never held across
// the call (State() needs s.mu every second).
func (s *Server) startTunnel() {
	s.tunnelMu.Lock()
	defer s.tunnelMu.Unlock()
	s.startTunnelLocked()
}

// startTunnelLocked is startTunnel's body; the caller must hold tunnelMu.
func (s *Server) startTunnelLocked() {
	provider := s.cfg.Tunnel
	if provider == "" {
		return
	}

	s.log("Starting tunnel: " + provider)
	tun, err := tunnel.Start(s.srvCtx, provider, s.cfg.Port)
	if err != nil {
		s.setTunnelError(fmt.Sprintf("%s tunnel failed: %v", provider, err))
		s.log("Tunnel error: " + err.Error())
		return
	}

	// If the server was stopped while the tunnel was coming up, don't keep the
	// freshly-started process — kill it now or we leak cloudflared/tailscale.
	if s.srvCtx.Err() != nil {
		tun.Stop()
		return
	}

	s.mu.Lock()
	s.tun = tun
	s.streamURL = tun.StreamURL()
	s.tunnelErr = ""
	s.mu.Unlock()
	s.log("Tunnel URL: " + tun.StreamURL())

	// Detect the tunnel dying mid-stream so a dead URL becomes visible (and
	// retryable) instead of silently failing.
	go s.monitorTunnel(tun, s.srvCtx)
}

// RestartTunnel tears down any existing tunnel and starts a fresh one with the
// current config. This is the recovery primitive: use it after signing into a
// provider, switching providers, or when a tunnel has died — all without
// dropping the stream. Returns an error describing the failure if the new
// tunnel doesn't come up.
func (s *Server) RestartTunnel() error {
	s.mu.Lock()
	streaming := s.status == "streaming"
	provider := s.cfg.Tunnel
	s.mu.Unlock()
	if !streaming {
		return fmt.Errorf("not streaming")
	}
	if provider == "" {
		return fmt.Errorf("no tunnel provider selected")
	}

	s.tunnelMu.Lock()
	defer s.tunnelMu.Unlock()

	// Drop the old tunnel and revert to the local URL while reconnecting.
	s.mu.Lock()
	old := s.tun
	s.tun = nil
	s.streamURL = s.localURL
	s.tunnelErr = ""
	s.mu.Unlock()
	if old != nil {
		old.Stop()
	}

	s.startTunnelLocked()

	s.mu.Lock()
	tErr := s.tunnelErr
	s.mu.Unlock()
	if tErr != "" {
		return fmt.Errorf("%s", tErr)
	}
	return nil
}

// setTunnelError records a tunnel failure and falls back to the local URL so
// LAN viewers keep working. The UI surfaces TunnelError and offers a retry.
func (s *Server) setTunnelError(msg string) {
	s.mu.Lock()
	s.tunnelErr = msg
	if s.localURL != "" {
		s.streamURL = s.localURL
	}
	s.mu.Unlock()
}

// monitorTunnel watches a tunnel for an unexpected mid-stream exit. It only
// surfaces the failure if this is still the active tunnel and we're still
// streaming — so an intentional Stop or a RestartTunnel swap stays quiet.
func (s *Server) monitorTunnel(tun *tunnel.Tunnel, ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-tun.Done:
		s.mu.Lock()
		active := s.tun == tun && s.status == "streaming"
		provider := s.cfg.Tunnel
		s.mu.Unlock()
		if active && ctx.Err() == nil {
			s.log("Tunnel process exited unexpectedly")
			s.setTunnelError(provider + " tunnel stopped unexpectedly — click Retry to reconnect")
		}
	}
}

// stopTunnel tears down the active tunnel under tunnelMu so it can't race a
// concurrent startTunnel/RestartTunnel and orphan a tunnel process.
func (s *Server) stopTunnel() {
	s.tunnelMu.Lock()
	s.mu.Lock()
	tun := s.tun
	s.tun = nil
	s.mu.Unlock()
	if tun != nil {
		tun.Stop()
	}
	s.tunnelMu.Unlock()
}

// Stop gracefully stops the entire streaming pipeline.
func (s *Server) Stop() error {
	s.mu.Lock()
	if s.status == "idle" {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	s.log("Stopping stream...")

	// Stop the tunnel FIRST, while its context is still live. The provider may
	// be launched via a shim that spawns the real binary as a child; the
	// process-tree kill must run before context cancellation kills only the
	// parent and orphans the child. Serialized with startTunnel/RestartTunnel.
	s.stopTunnel()
	// Cancel server context (stops FFmpeg, janitor, audio)
	if s.srvCancel != nil {
		s.srvCancel()
	}
	// Wait for FFmpeg to exit
	if s.ffmpegDone != nil {
		<-s.ffmpegDone
	}
	// Close audio pipe read-end (write-end is closed by AsyncWriter)
	if s.audioPipe != nil {
		s.audioPipe.Close()
		s.audioPipe = nil
	}
	s.audio = nil
	// Shut down HTTP server
	if s.httpSrv != nil {
		s.httpSrv.Shutdown(context.Background())
	}
	// Remove this session's temp segment dir (FFmpeg has already exited above).
	if s.segDir != "" {
		os.RemoveAll(s.segDir)
		s.segDir = ""
	}

	s.mu.Lock()
	s.status = "idle"
	s.errMsg = ""
	s.mu.Unlock()
	s.log("Stream stopped")
	s.closeSessionLog()
	return nil
}

// State returns the current stream state.
func (s *Server) State() StreamState {
	s.mu.Lock()
	state := StreamState{
		Status:      s.status,
		Error:       s.errMsg,
		StreamURL:   s.streamURL,
		TunnelError: s.tunnelErr,
	}
	if s.status == "streaming" {
		state.Uptime = time.Since(s.startTime)
	}
	s.mu.Unlock()

	if s.stats != nil {
		es := s.stats.Latest()
		state.FPS = es.FPS
		state.Bitrate = es.Bitrate
		state.DroppedFrames = es.DroppedFrames
		state.Speed = es.Speed
	}
	if s.hlsSrv != nil {
		state.ViewerCount = s.hlsSrv.ViewerCount()
	}

	return state
}

// Config returns the current configuration.
func (s *Server) Config() config.Config {
	return s.cfg
}

// SetConfig updates the configuration (only effective before next Start).
func (s *Server) SetConfig(cfg config.Config) {
	s.cfg = cfg
}

// LogEntries returns a copy of all log entries.
func (s *Server) LogEntries() []LogEntry {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	entries := make([]LogEntry, len(s.logEntries))
	copy(entries, s.logEntries)
	return entries
}

// failStream is called when FFmpeg exits unexpectedly during streaming.
// It sets the error state and cleans up all server resources so that
// the user can start a new stream without restarting the app.
func (s *Server) failStream(msg string) {
	s.log(msg)
	s.mu.Lock()
	s.status = "error"
	s.errMsg = msg
	s.mu.Unlock()

	// Stop the tunnel first, while its context is still live, so the
	// process-tree kill reaches any shim-spawned child (see Stop).
	s.stopTunnel()
	// Cancel server context (stops audio capturer, janitor)
	if s.srvCancel != nil {
		s.srvCancel()
	}
	// Wait for FFmpeg to exit before closing the audio pipe
	if s.ffmpegDone != nil {
		<-s.ffmpegDone
	}
	// Close audio pipe read-end (write-end is closed by AsyncWriter)
	if s.audioPipe != nil {
		s.audioPipe.Close()
		s.audioPipe = nil
	}
	s.audio = nil
	// Shut down HTTP server to free the port
	if s.httpSrv != nil {
		s.httpSrv.Shutdown(context.Background())
	}
	// Remove this session's temp segment dir (FFmpeg has already exited above).
	if s.segDir != "" {
		os.RemoveAll(s.segDir)
		s.segDir = ""
	}
	s.closeSessionLog()
}

func (s *Server) setError(msg string) {
	s.mu.Lock()
	s.status = "error"
	s.errMsg = msg
	s.mu.Unlock()
	s.log("Error: " + msg)
}

func (s *Server) log(msg string) {
	now := time.Now()
	s.logMu.Lock()
	s.logSeq++
	s.logEntries = append(s.logEntries, LogEntry{
		Time:    now,
		Message: msg,
	})
	// Keep only the most recent maxLogEntries in memory. The full history is
	// still written to the session log file below.
	if len(s.logEntries) > maxLogEntries {
		s.logEntries = s.logEntries[len(s.logEntries)-maxLogEntries:]
	}
	if s.logFile != nil {
		fmt.Fprintf(s.logFile, "%s  %s\n", now.Format("15:04:05"), msg)
	}
	s.logMu.Unlock()
	log.Println(msg)
}

// LogSeq returns the monotonic count of log lines emitted so far. The GUI uses
// it to skip re-sending an unchanged log buffer to the frontend every tick.
func (s *Server) LogSeq() uint64 {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	return s.logSeq
}

// cleanupStaleSegmentDirs removes leftover "vrshare-segments-*" directories in
// tempDir whose contents have not been modified within maxAge, skipping the
// keep path (the current session's dir). Returns the number removed.
func cleanupStaleSegmentDirs(tempDir, keep string, maxAge time.Duration) int {
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "vrshare-segments-") {
			continue
		}
		full := filepath.Join(tempDir, e.Name())
		if full == keep {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.RemoveAll(full) == nil {
			removed++
		}
	}
	return removed
}

func (s *Server) openSessionLog() {
	dir, err := DataDir()
	if err != nil {
		return
	}
	logsDir := filepath.Join(dir, "logs")
	os.MkdirAll(logsDir, 0755)
	name := fmt.Sprintf("session-%s.log", time.Now().Format("2006-01-02_15-04-05"))
	f, err := os.Create(filepath.Join(logsDir, name))
	if err != nil {
		return
	}
	s.logFile = f
}

func (s *Server) closeSessionLog() {
	s.logMu.Lock()
	if s.logFile != nil {
		s.logFile.Close()
		s.logFile = nil
	}
	s.logMu.Unlock()
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "localhost"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
