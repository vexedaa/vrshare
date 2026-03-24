package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"github.com/vexedaa/vrshare/internal/config"
	"github.com/vexedaa/vrshare/internal/ffmpeg"
	"github.com/vexedaa/vrshare/internal/hls"
	"github.com/vexedaa/vrshare/internal/tunnel"
)

func main() {
	cfg := config.Default()

	flag.IntVar(&cfg.Port, "port", cfg.Port, "HTTP server port")
	flag.BoolVar(&cfg.Tunnel, "tunnel", cfg.Tunnel, "Enable Cloudflare tunnel")
	flag.IntVar(&cfg.Monitor, "monitor", cfg.Monitor, "Monitor index to capture (0 = primary)")
	flag.IntVar(&cfg.FPS, "fps", cfg.FPS, "Capture framerate")
	flag.StringVar(&cfg.Resolution, "resolution", cfg.Resolution, "Output resolution (WxH, empty for native)")
	flag.IntVar(&cfg.Bitrate, "bitrate", cfg.Bitrate, "Video bitrate in kbps")
	encoder := flag.String("encoder", string(cfg.Encoder), "Encoder: auto, nvenc, qsv, amf, cpu")
	flag.Parse()

	cfg.Encoder = config.EncoderType(*encoder)

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Find or download FFmpeg
	ffmpegPath, err := ffmpeg.FindFFmpeg()
	if err != nil {
		log.Println(err)
		ffmpegPath, err = ffmpeg.PromptAndDownload()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
	log.Printf("Using FFmpeg: %s", ffmpegPath)

	// Detect encoder
	probe := ffmpeg.ProbeFFmpegEncoder(ffmpegPath)
	resolvedEncoder := ffmpeg.ResolveEncoder(string(cfg.Encoder), probe)
	log.Printf("Using encoder: %s", resolvedEncoder)

	// Probe for ddagrab (DXGI Desktop Duplication) support
	useDDAgrab := ffmpeg.ProbeDDAgrab(ffmpegPath)
	if useDDAgrab {
		log.Println("Using ddagrab (DXGI Desktop Duplication) for capture")
	} else {
		log.Println("Using gdigrab for capture (ddagrab not available)")
	}

	// Warn if resolution scaling is set with GPU encoder + ddagrab
	if useDDAgrab && cfg.Resolution != "" && resolvedEncoder != "cpu" {
		log.Printf("Warning: --resolution is ignored with GPU encoder + ddagrab. Use native resolution or --encoder cpu.")
	}

	// Create temp directory for segments
	segmentDir, err := os.MkdirTemp("", "vrshare-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(segmentDir)

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		signal.Stop(sigCh)
		log.Println("Shutting down...")
		cancel()
	}()

	// Start HLS server
	hlsServer := hls.NewServer(segmentDir)
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: port %d already in use. Try --port <number>\n", cfg.Port)
		os.Exit(1)
	}

	httpServer := &http.Server{Handler: hlsServer}
	go func() {
		log.Printf("HLS server listening on http://localhost:%d", cfg.Port)
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Print stream URLs
	localURL := fmt.Sprintf("http://localhost:%d/stream.m3u8", cfg.Port)
	lanIP := getOutboundIP()
	lanURL := fmt.Sprintf("http://%s:%d/stream.m3u8", lanIP, cfg.Port)

	fmt.Println()
	fmt.Println("=== VRShare ===")
	fmt.Printf("  Local:  %s\n", localURL)
	fmt.Printf("  LAN:    %s\n", lanURL)

	// Start tunnel if requested
	var tun *tunnel.Tunnel
	if cfg.Tunnel {
		log.Println("Starting Cloudflare tunnel...")
		tun, err = tunnel.Start(ctx, cfg.Port)
		if err != nil {
			log.Printf("Warning: tunnel failed: %v (continuing without tunnel)", err)
		} else {
			streamURL := tun.StreamURL()
			fmt.Printf("  Tunnel: %s\n", streamURL)
			if clipErr := copyToClipboard(streamURL); clipErr == nil {
				fmt.Println()
				fmt.Println("Stream URL copied to clipboard!")
			}
		}
	}

	fmt.Println()
	fmt.Println("Paste the URL above into any VRChat video player.")
	fmt.Println("Press Ctrl+C to stop.")
	fmt.Println()

	// Start segment janitor
	go hls.RunJanitor(ctx, segmentDir, 5*time.Second)

	// Build and run FFmpeg
	manager := ffmpeg.NewManager(ffmpegPath, segmentDir)
	args := ffmpeg.BuildArgs(cfg, resolvedEncoder, segmentDir, useDDAgrab)

	err = manager.Run(ctx, args)

	// Cleanup
	log.Println("Cleaning up...")
	if tun != nil {
		tun.Stop()
	}
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	httpServer.Shutdown(shutCtx)

	if err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	log.Println("Goodbye!")
}

// getOutboundIP returns the preferred outbound IP for LAN display.
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// copyToClipboard copies text to the system clipboard using clip.exe on Windows.
func copyToClipboard(text string) error {
	cmd := exec.Command("clip")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
