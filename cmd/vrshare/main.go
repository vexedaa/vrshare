package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/vexedaa/vrshare/internal/config"
	"github.com/vexedaa/vrshare/internal/server"
)

func main() {
	// Mode detection: no args + no console = GUI mode
	if len(os.Args) == 1 && !hasConsole() {
		launchGUI()
		return
	}

	// CLI mode
	cfg := config.Default()

	flag.IntVar(&cfg.Port, "port", cfg.Port, "HTTP server port")
	flag.StringVar(&cfg.Tunnel, "tunnel", cfg.Tunnel, "Tunnel provider: cloudflare, tailscale (empty = disabled)")
	flag.IntVar(&cfg.Monitor, "monitor", cfg.Monitor, "Monitor index to capture (0 = primary)")
	flag.IntVar(&cfg.FPS, "fps", cfg.FPS, "Capture framerate")
	flag.StringVar(&cfg.Resolution, "resolution", cfg.Resolution, "Output resolution (WxH, empty for native)")
	flag.IntVar(&cfg.Bitrate, "bitrate", cfg.Bitrate, "Video bitrate in kbps")
	encoder := flag.String("encoder", string(cfg.Encoder), "Encoder: auto, nvenc, qsv, amf, cpu")
	flag.BoolVar(&cfg.Audio, "audio", cfg.Audio, "Enable system audio capture (excludes VRChat)")
	flag.StringVar(&cfg.AudioDevice, "audio-device", cfg.AudioDevice, "Specific audio device name")
	flag.Parse()

	cfg.Encoder = config.EncoderType(*encoder)

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

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

	// Start server
	srv := server.New(cfg)
	if err := srv.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Display stream info
	state := srv.State()
	fmt.Println()
	fmt.Println("=== VRShare ===")
	fmt.Printf("  Stream: %s\n", state.StreamURL)
	if err := copyToClipboard(state.StreamURL); err == nil {
		fmt.Println("  URL copied to clipboard!")
	}
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop.")
	fmt.Println()

	// Wait for interrupt
	<-ctx.Done()
	srv.Stop()

	log.Println("Goodbye!")
}

// copyToClipboard copies text to the system clipboard using clip.exe on Windows.
func copyToClipboard(text string) error {
	cmd := exec.Command("clip")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
