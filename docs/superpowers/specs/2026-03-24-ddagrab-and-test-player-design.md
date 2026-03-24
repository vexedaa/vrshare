# ddagrab Capture + Test Player Design Spec

## Purpose

Two changes to VRShare:
1. Switch Windows screen capture from `gdigrab` (CPU-heavy GDI) to `ddagrab` (DXGI Desktop Duplication) for dramatically lower resource usage. With GPU encoders, the entire capture-to-encode pipeline stays on the GPU.
2. Add a built-in browser test page at `/` so users can verify playback without external tools.

## 1. Performance Fix: ddagrab Capture

### Problem

`gdigrab` captures via the legacy GDI API, copying every frame through the CPU. This causes excessive CPU usage that makes the system unusable, especially at high resolutions/framerates. OBS uses DXGI Desktop Duplication for near-zero CPU capture cost.

### Solution

Replace `gdigrab` with `ddagrab` (available in FFmpeg 7.0+). `ddagrab` captures as D3D11 hardware textures, which GPU encoders can consume directly without any CPU involvement.

### Command Changes

**GPU encoders (nvenc/qsv/amf) — frames stay on GPU:**
```
ffmpeg -f ddagrab -framerate 30 -i 0 \
  -c:v h264_nvenc -preset p4 -tune ll \
  -b:v 4000k -g 30 -keyint_min 30 \
  -f hls -hls_time 1 -hls_list_size 3 \
  -hls_flags delete_segments+append_list \
  -hls_segment_filename <dir>/segment_%d.ts \
  <dir>/stream.m3u8
```

**CPU encoder (libx264) — hwdownload to convert to software frames:**
```
ffmpeg -f ddagrab -framerate 30 -i 0 \
  -vf hwdownload,format=bgra,format=yuv420p \
  -c:v libx264 -preset veryfast -tune zerolatency \
  -b:v 4000k -g 30 -keyint_min 30 \
  -f hls ...
```

**CPU encoder + resolution scaling:**
```
-vf hwdownload,format=bgra,format=yuv420p,scale=1280:720
```

**GPU encoder + resolution scaling:** Not supported in this change. Use native resolution. Can add GPU-side scaling later.

### Input Source

- `ddagrab` uses integer monitor index: `-i 0` (primary), `-i 1` (secondary), etc.
- This aligns with the existing `--monitor` CLI flag (already integer-based)

### Fallback

- At startup, probe for `ddagrab` support by checking FFmpeg's available input formats (`ffmpeg -devices`)
- If `ddagrab` is unavailable (FFmpeg < 7.0), fall back to `gdigrab` with a warning
- Non-Windows platforms are unaffected (still use `x11grab`/`avfoundation`)

### Files Changed

- `internal/ffmpeg/command.go` — update `BuildArgs` for ddagrab paths
- `internal/ffmpeg/command_test.go` — tests for ddagrab args, hwdownload chain, fallback

## 2. Test Player Page

### Solution

Serve a minimal HTML page at `http://localhost:<port>/` that uses hls.js from CDN to play the stream.

### Implementation

- Add a `const playerHTML` in `internal/hls/server.go` containing the HTML
- In `ServeHTTP`, when path is `/` (or `/index.html`), serve the HTML with `Content-Type: text/html`
- The HTML loads hls.js from `https://cdn.jsdelivr.net/npm/hls.js@latest`, attaches to a `<video>` element, points at `/stream.m3u8`, and auto-plays
- Dark background, centered video element, minimal styling

### Files Changed

- `internal/hls/server.go` — add player HTML const and `/` route
- `internal/hls/server_test.go` — test that `/` returns HTML with correct content type

## Testing

### Unit Tests

- `BuildArgs` with ddagrab: verify `-f ddagrab -i <monitor>` for Windows
- `BuildArgs` with ddagrab + CPU encoder: verify hwdownload filter chain
- `BuildArgs` with ddagrab + CPU encoder + resolution: verify combined filter chain
- `BuildArgs` with ddagrab + GPU encoder: verify NO hwdownload in args
- `BuildArgs` with gdigrab fallback: verify old behavior when ddagrab unavailable
- HLS server `/` route: returns 200 with `text/html` content type and contains `hls.js`

### Manual Smoke Test

- Run `vrshare` and open `http://localhost:8080/` in browser — should show video player auto-playing the stream
- Compare CPU usage vs previous gdigrab version — should be dramatically lower with GPU encoder
