package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/vexedaa/vrshare/internal/config"
	"github.com/vexedaa/vrshare/internal/ffmpeg"
	"github.com/vexedaa/vrshare/internal/hls"
	"github.com/vexedaa/vrshare/internal/tunnel"
)

// Server orchestrates the streaming pipeline: FFmpeg, HLS, audio, and tunnel.
type Server struct {
	cfg        config.Config
	mu         sync.Mutex
	status     string
	startTime  time.Time
	streamURL  string
	errMsg     string

	// Server-level context (HLS, janitor, tunnel, audio)
	srvCancel  context.CancelFunc
	srvCtx     context.Context

	// FFmpeg-level context (can be restarted independently)
	ffmpegCancel context.CancelFunc
	ffmpegDone   chan struct{}

	hlsSrv     *hls.Server
	httpSrv    *http.Server
	tun        *tunnel.Tunnel
	stats      *StatsParser
	ffmpegPath string
	useDDAgrab bool
	encoder    string
	segDir     string
	audioPipe  *os.File
	logEntries []LogEntry
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
	probe := ffmpeg.ProbeFFmpegEncoder(ffmpegPath)
	s.encoder = ffmpeg.ResolveEncoder(string(s.cfg.Encoder), probe)
	s.useDDAgrab = ffmpeg.ProbeDDAgrab(ffmpegPath)
	s.log(fmt.Sprintf("Encoder: %s, DDAgrab: %v", s.encoder, s.useDDAgrab))

	// Create temp segment directory
	segDir, err := os.MkdirTemp("", "vrshare-segments-*")
	if err != nil {
		s.setError("Failed to create segment dir: " + err.Error())
		return err
	}
	s.segDir = segDir

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
	s.streamURL = fmt.Sprintf("http://%s:%d/stream.m3u8", ip, s.cfg.Port)
	s.mu.Unlock()
	s.log("Stream URL: " + s.streamURL)

	// Server-level context for long-lived services
	s.srvCtx, s.srvCancel = context.WithCancel(ctx)

	go hls.RunJanitor(s.srvCtx, segDir, s.hlsSrv, 2*time.Second)

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
		ac = newAudioCapturer(s.srvCtx, w)
	}

	// Start tunnel if configured
	if s.cfg.Tunnel != "" {
		s.log("Starting tunnel: " + s.cfg.Tunnel)
		tun, err := tunnel.Start(s.srvCtx, s.cfg.Tunnel, s.cfg.Port)
		if err != nil {
			s.log("Tunnel warning: " + err.Error())
		} else {
			s.tun = tun
			s.mu.Lock()
			s.streamURL = tun.StreamURL()
			s.mu.Unlock()
			s.log("Tunnel URL: " + s.streamURL)
		}
	}

	// Start FFmpeg first, then audio capturer — this order prevents stale
	// audio from accumulating in the buffer during tunnel/startup delays.
	if err := s.startFFmpeg(); err != nil {
		s.srvCancel()
		s.httpSrv.Shutdown(context.Background())
		return err
	}
	if ac != nil {
		go ac.start(s.srvCtx)
	}

	s.mu.Lock()
	s.status = "streaming"
	s.startTime = time.Now()
	s.mu.Unlock()
	s.log("Stream started")

	return nil
}

// startFFmpeg launches the FFmpeg process with current config.
func (s *Server) startFFmpeg() error {
	args := ffmpeg.BuildArgs(s.cfg, s.encoder, s.segDir, s.useDDAgrab)
	mgr := ffmpeg.NewManager(s.ffmpegPath, s.segDir)

	s.stats = NewStatsParser(os.Stderr)
	s.stats.LogFunc = func(line string) { s.log("FFmpeg: " + line) }
	mgr.StderrWriter = s.stats

	ffCtx, ffCancel := context.WithCancel(s.srvCtx)
	s.ffmpegCancel = ffCancel
	s.ffmpegDone = make(chan struct{})

	go func() {
		defer close(s.ffmpegDone)
		err := mgr.Run(ffCtx, args, s.audioPipe)
		// If the FFmpeg context was cancelled, this is intentional
		// (user clicked Stop, app is closing, or RestartCapture was called).
		if ffCtx.Err() != nil {
			return
		}
		// FFmpeg exited while we were still supposed to be streaming.
		// Clean up all resources so the user can start a new stream.
		msg := "Stream ended unexpectedly"
		if err != nil {
			msg = "FFmpeg error: " + err.Error()
		}
		s.failStream(msg)
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
	probe := ffmpeg.ProbeFFmpegEncoder(s.ffmpegPath)
	s.encoder = ffmpeg.ResolveEncoder(string(s.cfg.Encoder), probe)

	// Relaunch FFmpeg
	if err := s.startFFmpeg(); err != nil {
		s.setError("Failed to restart capture: " + err.Error())
		return err
	}

	s.log("Capture restarted")
	return nil
}

// Stop gracefully stops the entire streaming pipeline.
func (s *Server) Stop() error {
	s.mu.Lock()
	if s.status == "idle" {
		s.mu.Unlock()
		return nil
	}
	tun := s.tun
	s.tun = nil
	s.mu.Unlock()

	s.log("Stopping stream...")

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
	// Explicitly stop tunnel process (don't rely on context alone)
	if tun != nil {
		tun.Stop()
	}
	// Shut down HTTP server
	if s.httpSrv != nil {
		s.httpSrv.Shutdown(context.Background())
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
		Status:    s.status,
		Error:     s.errMsg,
		StreamURL: s.streamURL,
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
	tun := s.tun
	s.tun = nil
	s.mu.Unlock()

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
	// Stop tunnel process
	if tun != nil {
		tun.Stop()
	}
	// Shut down HTTP server to free the port
	if s.httpSrv != nil {
		s.httpSrv.Shutdown(context.Background())
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
	s.logEntries = append(s.logEntries, LogEntry{
		Time:    now,
		Message: msg,
	})
	if s.logFile != nil {
		fmt.Fprintf(s.logFile, "%s  %s\n", now.Format("15:04:05"), msg)
	}
	s.logMu.Unlock()
	log.Println(msg)
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
