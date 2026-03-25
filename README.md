# VRShare

Stream your desktop directly into VRChat. No YouTube, no Twitch — just paste a URL into any VRChat video player and your screen is live.

VRShare captures your desktop using GPU-accelerated screen capture, encodes with hardware video encoders, and serves an HLS stream that any VRChat video player can consume. A built-in GUI makes setup simple, and optional tunnel support gives you a public URL without port forwarding.

## Download

Download the latest `vrshare.exe` from [GitHub Releases](https://github.com/vexedaa/vrshare/releases). No installation needed — just download and run.

### Requirements

- **Windows 10/11** (x64)
- **FFmpeg** — VRShare will prompt to download it on first run if not found
- **WebView2 Runtime** — included with Windows 10/11 and Microsoft Edge

### Optional

- **[cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)** — for Cloudflare tunnel support
- **[Tailscale](https://tailscale.com/download)** — for Tailscale Funnel support

## Getting Started

1. **Double-click `vrshare.exe`** to open the GUI
2. **First-run wizard** auto-detects your GPU encoder, monitors, and audio — confirm or adjust
3. **Click "Start Stream"** on the dashboard
4. **Copy the stream URL** and paste it into any VRChat video player

That's it. Your desktop is now streaming.

## Features

- **GUI with system tray** — configure, start, and stop from a graphical interface
- **GPU-native capture** — DXGI Desktop Duplication via ddagrab (zero CPU copy)
- **Hardware encoding** — NVIDIA NVENC, Intel Quick Sync, AMD AMF (auto-detected, software fallback)
- **HLS streaming** — compatible with all VRChat video players (ProTV, AVPro, etc.)
- **System audio** — WASAPI loopback capture with automatic VRChat exclusion
- **Tunnel support** — Cloudflare or Tailscale for public HTTPS URLs without port forwarding
- **Quick display switching** — switch monitors with one click while streaming
- **Presets** — save and load named configurations
- **CLI mode** — pass command-line flags for headless/scripted use

## GUI Mode

Double-click `vrshare.exe` (or launch it without arguments from Explorer). The app opens with:

- **Dashboard** — start/stop stream, see stream URL, viewer count, and event log
- **Settings** — configure video (encoder, monitor, resolution, FPS, bitrate), audio, network (port, tunnel), presets, and tunnel provider authorization
- **System tray** — minimizes to tray when closed (configurable). Right-click for quick actions, double-click to restore.
- **Display switcher** — numbered buttons on the dashboard to switch monitors without opening settings

Settings changes while streaming automatically restart the capture without dropping the stream URL or tunnel connection.

## CLI Mode

Launch from a terminal with flags to run headless (no GUI):

```
vrshare.exe [flags]

Flags:
  --port int            HTTP server port (default 8080)
  --tunnel string       Tunnel provider: cloudflare, tailscale (default: disabled)
  --monitor int         Monitor index to capture, 0 = primary (default 0)
  --fps int             Capture framerate (default 30)
  --resolution string   Output resolution, WxH (default: native)
  --bitrate int         Video bitrate in kbps (default 4000)
  --encoder string      Encoder: auto, nvenc, qsv, amf, cpu (default "auto")
  --audio               Enable system audio capture
  --audio-device string Specific audio device name
```

### Examples

```bash
# Start with defaults
vrshare.exe

# 60fps at 8mbps with Cloudflare tunnel and audio
vrshare.exe --fps 60 --bitrate 8000 --tunnel cloudflare --audio

# Capture second monitor at 720p
vrshare.exe --monitor 1 --resolution 1280x720

# Use Tailscale Funnel
vrshare.exe --tunnel tailscale --audio
```

The stream URL is printed to the terminal and copied to your clipboard.

## Tunnel Setup

Tunnels give you a public HTTPS URL so viewers outside your local network can watch — no port forwarding needed.

### Cloudflare (no account required)

1. Install `cloudflared`: `choco install cloudflared` or [download here](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)
2. Select "Cloudflare" as tunnel provider in Settings (or use `--tunnel cloudflare`)
3. A temporary public URL is generated automatically

### Tailscale Funnel

1. Install [Tailscale](https://tailscale.com/download) and sign in
2. In VRShare Settings > Tunnel Providers, click "Sign In" for Tailscale if needed
3. Select "Tailscale" as tunnel provider in Settings (or use `--tunnel tailscale`)
4. Your stable Tailscale Funnel URL is used automatically

## FFmpeg

VRShare requires FFmpeg for screen capture and encoding. On first run, it will offer to download a compatible build automatically.

### Custom FFmpeg builds

For the best performance, use an FFmpeg build with **ddagrab** support (DXGI Desktop Duplication — GPU-native capture with zero CPU copy). Place your custom `ffmpeg.exe` in `~/.vrshare/ffmpeg/` and it will be used automatically.

Without ddagrab, VRShare falls back to gdigrab (GDI capture), which works but uses more CPU.

## How It Works

```
Desktop Screen
    |
    v
ddagrab (DXGI Desktop Duplication) -- GPU capture, zero CPU copy
    |
    v
h264_nvenc (GPU encoder) -- or QSV/AMF/libx264
    |
    v
HLS segments (.ts) + playlist (.m3u8) -- 1-second segments
    |
    v
Go HTTP server -- CORS, caching, built-in test player at /
    |
    +--> LAN: http://192.168.x.x:8080/stream.m3u8
    +--> Tunnel: https://your-url.trycloudflare.com/stream.m3u8
    |
    v
VRChat Video Player (ProTV, AVPro, etc.)
```

## Building from Source

Requires Go 1.24+, Node.js 18+, and npm.

```powershell
# Clone
git clone https://github.com/vexedaa/vrshare.git
cd vrshare

# Build (GUI + CLI)
.\build.ps1
```

This produces `vrshare.exe` with the embedded GUI frontend and Windows icon.

For CLI-only builds (no Wails/GUI):

```bash
go build -o vrshare.exe ./cmd/vrshare/
```

## Data Location

VRShare stores configuration in `~/.vrshare/`:

```
~/.vrshare/
  config.json      -- current settings
  settings.json    -- app preferences (close behavior, first-run flag)
  presets/          -- saved presets
  ffmpeg/           -- FFmpeg binary (auto-downloaded or custom)
  debug.log         -- GUI debug log
```

## License

MIT
