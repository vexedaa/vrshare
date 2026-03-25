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
	cancel     context.CancelFunc
	done       chan struct{}
	hlsSrv     *hls.Server
	stats      *StatsParser
	ffmpegPath string
	useDDAgrab bool
	encoder    string
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

	// Start HLS server
	s.hlsSrv = hls.NewServer(segDir)
	s.hlsSrv.SetMP4Support(ffmpegPath, s.cfg.Port)
	httpSrv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.cfg.Port),
		Handler: s.hlsSrv,
	}

	go func() {
		if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Build stream URL
	ip := getOutboundIP()
	s.mu.Lock()
	s.streamURL = fmt.Sprintf("http://%s:%d/stream.m3u8", ip, s.cfg.Port)
	s.mu.Unlock()
	s.log("Stream URL: " + s.streamURL)

	// Start janitor
	srvCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})

	go hls.RunJanitor(srvCtx, segDir, s.hlsSrv, 5*time.Second)

	// Start audio if enabled
	var audioPipe *os.File
	if s.cfg.Audio {
		s.log("Audio capture enabled")
		r, w, err := os.Pipe()
		if err != nil {
			cancel()
			httpSrv.Shutdown(context.Background())
			s.setError("Failed to create audio pipe: " + err.Error())
			return err
		}
		audioPipe = r
		ac := newAudioCapturer(w)
		go ac.start(srvCtx)
	}

	// Start tunnel if configured
	if s.cfg.Tunnel != "" {
		s.log("Starting tunnel: " + s.cfg.Tunnel)
		tun, err := tunnel.Start(srvCtx, s.cfg.Tunnel, s.cfg.Port)
		if err != nil {
			s.log("Tunnel warning: " + err.Error())
		} else {
			s.mu.Lock()
			s.streamURL = tun.StreamURL()
			s.mu.Unlock()
			s.log("Tunnel URL: " + s.streamURL)
		}
	}

	// Build FFmpeg args and start
	args := ffmpeg.BuildArgs(s.cfg, s.encoder, segDir, s.useDDAgrab)
	mgr := ffmpeg.NewManager(ffmpegPath, segDir)

	// Hook stats parser
	s.stats = NewStatsParser(os.Stderr)
	mgr.StderrWriter = s.stats

	s.mu.Lock()
	s.status = "streaming"
	s.startTime = time.Now()
	s.mu.Unlock()
	s.log("Stream started")

	// Run FFmpeg in background
	go func() {
		defer close(s.done)
		if err := mgr.Run(srvCtx, args, audioPipe); err != nil {
			if srvCtx.Err() == nil {
				s.setError("FFmpeg error: " + err.Error())
			}
		}
		httpSrv.Shutdown(context.Background())
		mgr.Cleanup()
		s.mu.Lock()
		if s.status != "error" {
			s.status = "idle"
		}
		s.mu.Unlock()
	}()

	return nil
}

// Stop gracefully stops the streaming pipeline.
func (s *Server) Stop() error {
	s.mu.Lock()
	if s.status != "streaming" && s.status != "starting" {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	s.log("Stopping stream...")
	if s.cancel != nil {
		s.cancel()
	}
	if s.done != nil {
		<-s.done
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
