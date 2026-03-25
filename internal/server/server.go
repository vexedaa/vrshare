package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
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

	// Start HLS server
	s.hlsSrv = hls.NewServer(segDir)
	s.hlsSrv.SetMP4Support(ffmpegPath, s.cfg.Port)
	s.httpSrv = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.cfg.Port),
		Handler: s.hlsSrv,
	}

	go func() {
		if err := s.httpSrv.ListenAndServe(); err != http.ErrServerClosed {
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

	go hls.RunJanitor(s.srvCtx, segDir, s.hlsSrv, 5*time.Second)

	// Start audio if enabled
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
		ac := newAudioCapturer(w)
		go ac.start(s.srvCtx)
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

	// Start FFmpeg
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

// startFFmpeg launches the FFmpeg process with current config.
func (s *Server) startFFmpeg() error {
	args := ffmpeg.BuildArgs(s.cfg, s.encoder, s.segDir, s.useDDAgrab)
	mgr := ffmpeg.NewManager(s.ffmpegPath, s.segDir)

	s.stats = NewStatsParser(os.Stderr)
	mgr.StderrWriter = s.stats

	ffCtx, ffCancel := context.WithCancel(s.srvCtx)
	s.ffmpegCancel = ffCancel
	s.ffmpegDone = make(chan struct{})

	go func() {
		defer close(s.ffmpegDone)
		if err := mgr.Run(ffCtx, args, s.audioPipe); err != nil {
			if ffCtx.Err() == nil && s.srvCtx.Err() == nil {
				s.setError("FFmpeg error: " + err.Error())
			}
		}
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
	if s.status != "streaming" && s.status != "starting" {
		s.mu.Unlock()
		return nil
	}
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
	// Explicitly stop tunnel process (don't rely on context alone)
	if s.tun != nil {
		s.tun.Stop()
		s.tun = nil
	}
	// Shut down HTTP server
	if s.httpSrv != nil {
		s.httpSrv.Shutdown(context.Background())
	}

	s.mu.Lock()
	s.status = "idle"
	s.mu.Unlock()
	s.log("Stream stopped")
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

func (s *Server) setError(msg string) {
	s.mu.Lock()
	s.status = "error"
	s.errMsg = msg
	s.mu.Unlock()
	s.log("Error: " + msg)
}

func (s *Server) log(msg string) {
	s.logMu.Lock()
	s.logEntries = append(s.logEntries, LogEntry{
		Time:    time.Now(),
		Message: msg,
	})
	s.logMu.Unlock()
	log.Println(msg)
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "localhost"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
