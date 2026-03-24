package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/vexedaa/vrshare/internal/config"
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

	fmt.Printf("VRShare starting with config:\n")
	fmt.Printf("  Port:       %d\n", cfg.Port)
	fmt.Printf("  FPS:        %d\n", cfg.FPS)
	fmt.Printf("  Bitrate:    %d kbps\n", cfg.Bitrate)
	fmt.Printf("  Encoder:    %s\n", cfg.Encoder)
	if cfg.Resolution != "" {
		fmt.Printf("  Resolution: %s\n", cfg.Resolution)
	}
	fmt.Printf("  Monitor:    %d\n", cfg.Monitor)
	fmt.Printf("  Tunnel:     %v\n", cfg.Tunnel)
}
