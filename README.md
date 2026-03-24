# VRShare

Stream your desktop directly into VRChat. No YouTube, no Twitch — just paste a URL into any VRChat video player and your screen is live.

VRShare captures your desktop using DXGI Desktop Duplication (or GDI as fallback), encodes via hardware GPU encoder (NVENC/QSV/AMF), and serves an HLS stream over HTTP. Optional Cloudflare tunnel support gives you a public HTTPS URL without port forwarding.

## Features

- **GPU-native capture** — DXGI Desktop Duplication via ddagrab (zero CPU copy)
- **Hardware encoding** — NVENC, Quick Sync, AMF auto-detected (software fallback)
- **HLS streaming** — compatible with all VRChat video players (ProTV, AVPro, etc.)
- **Cloudflare tunnel** — public HTTPS URL with one flag, no port forwarding needed
- **System audio** — optional WASAPI loopback capture
- **Built-in test player** — verify your stream at `http://localhost:8080/`
- **Single binary** — one Go executable, FFmpeg as the only dependency

## Quick Start

```bash
# Build
go build -o vrshare.exe ./cmd/vrshare

# Start streaming (auto-detects GPU encoder)
./vrshare.exe

# Start with a public tunnel URL (copied to clipboard)
./vrshare.exe --tunnel

# Start with audio
./vrshare.exe --tunnel --audio
```

Paste the stream URL into any VRChat video player. Done.

## Usage

```
vrshare [flags]

Flags:
  --port int          HTTP server port (default 8080)
  --tunnel            Enable Cloudflare tunnel for public access
  --monitor int       Monitor index to capture, 0 = primary (default 0)
  --fps int           Capture framerate (default 30)
  --resolution string Output resolution, WxH (default: native)
  --bitrate int       Video bitrate in kbps (default 4000)
  --encoder string    Encoder: auto, nvenc, qsv, amf, cpu (default "auto")
  --audio             Enable system audio capture
  --audio-device string Audio device name (auto-detect if empty)
```

### Examples

```bash
# 60fps at 8mbps with NVENC
./vrshare.exe --fps 60 --bitrate 8000 --encoder nvenc

# Capture second monitor
./vrshare.exe --monitor 1

# Stream with audio and public URL
./vrshare.exe --tunnel --audio

# CPU encoding at 720p (if no GPU encoder available)
./vrshare.exe --encoder cpu --resolution 1280x720
```

## Requirements

- **Go 1.22+** for building
- **FFmpeg** with ddagrab support (recommended) or any FFmpeg build (gdigrab fallback)
- **cloudflared** (optional, for `--tunnel` support)

### FFmpeg with ddagrab

Pre-built FFmpeg binaries typically don't include ddagrab. For the best performance (zero-CPU-copy GPU pipeline), build FFmpeg from source with MSYS2:

```bash
# In MSYS2 UCRT64 terminal
pacman -S --needed base-devel mingw-w64-ucrt-x86_64-toolchain \
  mingw-w64-ucrt-x86_64-nasm mingw-w64-ucrt-x86_64-yasm git \
  mingw-w64-ucrt-x86_64-libx264 mingw-w64-ucrt-x86_64-ffnvcodec-headers \
  mingw-w64-ucrt-x86_64-amf-headers

git clone --depth 1 https://git.ffmpeg.org/ffmpeg.git && cd ffmpeg

./configure --enable-gpl --enable-nonfree --enable-libx264 \
  --enable-d3d11va --enable-dxva2 --enable-nvenc --enable-amf \
  --prefix=$HOME/ffmpeg-build

make -j$(nproc) && make install
```

Copy the built `ffmpeg.exe` and its DLLs to `~/.vrshare/ffmpeg/`:

```bash
cp ~/ffmpeg-build/bin/ffmpeg.exe ~/.vrshare/ffmpeg/
# Copy required DLLs from /ucrt64/bin/:
# libbz2-1.dll, libgcc_s_seh-1.dll, libwinpthread-1.dll,
# libiconv-2.dll, liblzma-5.dll, libstdc++-6.dll, libx264-*.dll, zlib1.dll
```

VRShare checks `~/.vrshare/ffmpeg/` before PATH, so your custom build is automatically preferred.

**Without ddagrab:** VRShare falls back to gdigrab (GDI capture), which works but uses significantly more CPU — especially at high resolutions or multi-monitor setups.

### Audio Setup

`--audio` captures system audio output via WASAPI loopback. It auto-detects common loopback devices (Stereo Mix, CABLE Output, etc.).

If no device is found:
1. **Enable Stereo Mix:** Sound Settings > Recording > right-click > Show Disabled Devices > enable "Stereo Mix"
2. **Or install [VB-Audio Virtual Cable](https://vb-audio.com/Cable/)** and set CABLE Output as default playback
3. **Or specify manually:** `--audio-device "Your Device Name"`

### Cloudflare Tunnel

Install `cloudflared`:
```bash
choco install cloudflared
# or download from https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/
```

Run with `--tunnel` and the public stream URL is automatically copied to your clipboard.

## How It Works

```
Desktop Screen
    |
    v
ddagrab (DXGI Desktop Duplication) -- GPU capture, zero CPU copy
    |
    v
h264_nvenc (NVIDIA GPU encoder) -- or QSV/AMF/libx264
    |
    v
HLS segments (.ts) + playlist (.m3u8) -- 1-second segments, 3-segment window
    |
    v
Go HTTP server -- CORS headers, cache control, test player at /
    |
    +---> LAN: http://192.168.x.x:8080/stream.m3u8
    +---> Tunnel: https://random.trycloudflare.com/stream.m3u8
    |
    v
VRChat Video Player (ProTV, AVPro, etc.)
```

## License

MIT
