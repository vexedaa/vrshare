# VRShare Design Spec

## Purpose

VRShare lets PC enthusiasts stream their screen directly into VRChat. The user runs a server on their machine that captures their desktop, encodes it as an HLS stream, and serves it over HTTP. Any VRChat video player (ProTV, AVPro, etc.) can play the stream by pasting a URL.

## Requirements

- Full desktop/monitor capture streamed as HLS
- Target latency: 2-4 seconds (real-time-ish)
- Compatible with all major VRChat video players (HLS is the common denominator)
- Windows-first, cross-platform possible later
- CLI interface for v1, GUI later
- Local + optional Cloudflare tunnel for public access
- Single Go binary, FFmpeg as the only external dependency (auto-downloaded if missing)
- Configurable: fps, resolution, bitrate, encoder, port, monitor selection

## Architecture

### Pipeline Overview

```
Windows Desktop
    |
    v
FFmpeg Subprocess (gdigrab capture -> H.264 encode -> HLS mux)
    |  writes .m3u8 + .ts segments to temp dir
    v
Go HTTP Server (serves HLS with CORS headers, cleans old segments)
    |
    +---> Direct LAN access: http://192.168.x.x:8080/stream.m3u8
    +---> Cloudflare Tunnel:  https://random-slug.trycloudflare.com/stream.m3u8
    |
    v
VRChat Video Player (ProTV / AVPro / any HLS-compatible player)
```

### Go Binary Components

The single Go binary contains four internal components:

**1. FFmpeg Manager**
- Locates FFmpeg on PATH
- If not found, prompts to download a static build to `~/.vrshare/ffmpeg/`
- Spawns FFmpeg with the correct capture/encode flags
- Monitors the process and auto-restarts on crash
- Probes for available hardware encoders at startup

**2. HLS Server**
- `net/http` server serving files from the temp segment directory
- CORS headers: `Access-Control-Allow-Origin: *`
- Cache headers: `Cache-Control: no-cache` on `.m3u8`, short cache on `.ts`
- Binds to configurable port (default: 8080)

**3. Segment Janitor**
- Goroutine watching the segment directory
- Deletes segments that have fallen out of the playlist window
- Keeps active disk usage under ~50MB

**4. Tunnel Manager (optional)**
- Activated via `--tunnel` flag
- Locates or downloads `cloudflared`
- Spawns a quick tunnel pointing at the local HTTP server
- Parses and displays the public URL
- Falls back to local-only mode if tunnel fails

## CLI Interface

```
vrshare                          # start with defaults
vrshare --port 9090              # custom port (default: 8080)
vrshare --tunnel                 # enable Cloudflare tunnel
vrshare --monitor 1              # capture specific monitor (default: primary)
vrshare --fps 60                 # framerate (default: 30)
vrshare --resolution 1920x1080   # scale output (default: native)
vrshare --bitrate 6000           # kbps (default: 4000)
vrshare --encoder auto           # auto/nvenc/qsv/amf/cpu (default: auto)
```

## FFmpeg Configuration

### Base Command (Windows)

```
ffmpeg -f gdigrab -framerate <fps> -i desktop \
  -c:v <encoder> <encoder_flags> \
  -b:v <bitrate>k \
  [-vf scale=<resolution>] \
  -g <fps> -keyint_min <fps> \
  -f hls -hls_time 1 -hls_list_size 3 \
  -hls_flags delete_segments+append_list \
  -hls_segment_filename <tmpdir>/segment_%d.ts \
  <tmpdir>/stream.m3u8
```

### Encoder Mappings

Auto-detection priority: NVENC -> QSV -> AMF -> CPU fallback.

| Encoder | FFmpeg codec       | Flags                                  |
|---------|--------------------|----------------------------------------|
| NVIDIA  | `h264_nvenc`       | `-preset p4 -tune ll`                  |
| Intel   | `h264_qsv`        | `-preset veryfast`                     |
| AMD     | `h264_amf`        | `-quality speed`                       |
| CPU     | `libx264`         | `-preset veryfast -tune zerolatency`   |

### Key Tuning Decisions

- **1-second HLS segments** with playlist window of 3 — gives 2-4 second end-to-end latency
- **Keyframe interval = framerate** — one keyframe per second for clean segment boundaries
- **Low-latency presets** across all encoders
- **No audio for v1** — simplifies the pipeline; can be added later with `-f dshow` input on Windows

### Platform-Specific Capture

| Platform | FFmpeg input            |
|----------|-------------------------|
| Windows  | `-f gdigrab -i desktop` |
| Linux    | `-f x11grab -i :0.0`   |
| macOS    | `-f avfoundation -i 1`  |

Windows is the primary target. Linux/macOS capture flags are noted for future cross-platform support.

## Error Handling

| Scenario                  | Behavior                                                        |
|---------------------------|-----------------------------------------------------------------|
| FFmpeg crashes            | Auto-restart; brief viewer interruption, player reconnects      |
| Port in use               | Clear error at startup suggesting `--port`                      |
| Cloudflare tunnel fails   | Log warning, fall back to local-only mode                       |
| Disk space                | Segment Janitor keeps usage under ~50MB                         |
| Graceful shutdown (Ctrl+C)| Kill FFmpeg, stop tunnel, remove temp dir, exit cleanly         |
| FFmpeg not found + download fails | Clear error with manual install instructions            |

## Testing Strategy

### Unit Tests (no FFmpeg required)

- CLI flag parsing and validation
- FFmpeg command construction — given config, assert correct args
- Segment Janitor logic — given files and playlist, assert correct cleanup
- Tunnel URL parsing from cloudflared output
- Encoder auto-detection logic (mocked FFmpeg probe responses)

### Integration Tests (FFmpeg required, gated with `//go:build integration`)

- Full pipeline start — verify `.m3u8` and `.ts` files appear
- HTTP server responds with correct CORS headers and content types
- Segment cleanup over time
- Graceful shutdown cleans up all resources

### Manual / Smoke Tests

- Playback in browser HLS player (hls.js)
- Playback in VRChat with ProTV / AVPro
- Tunnel connectivity from external network
- GPU encoder paths on different hardware

## Out of Scope (v1)

- Audio capture
- Application window / region capture
- GUI / tray application
- Authentication / access control
- Multiple simultaneous viewers (works but untested/unoptimized)
- Recording / VOD
