# Low-Latency HLS (LL-HLS) Design

## Problem

Video/audio stream latency through a tunnel is ~5 seconds. Target is 1-2 seconds.

## Approach

Layer two optimizations:

1. **Aggressive HLS tuning** — halve segment/GOP duration, tighten playlist and player buffers
2. **LL-HLS partial segments** — FFmpeg emits partial segments within each 0.5s segment; server supports blocking playlist requests so the player fetches new data immediately instead of polling

## Changes

### FFmpeg args (`internal/ffmpeg/command.go`)

| Parameter | Current | New |
|-----------|---------|-----|
| `-hls_time` | 1 | 0.5 |
| `-g` / `-keyint_min` | FPS | FPS/2 |
| `-hls_list_size` | 3 | 2 |
| `-hls_flags` | `append_list` | `append_list+delete_segments+low_latency` |
| `-flush_packets` | (unset) | 1 |
| `-hls_segment_type` | (unset, defaults to mpegts) | fmp4 |
| `-hls_fmp4_init_filename` | (unset) | `init.mp4` |
| segment filename ext | `.ts` | `.m4s` |

Encoder presets, bitrate, resolution, profile, and audio encoding are unchanged. The only quality trade-off is ~10-15% more bandwidth from doubling keyframe frequency, which is imperceptible at typical streaming bitrates.

### HLS server (`internal/hls/server.go`)

- Serve `.m4s` segments and `init.mp4` init segment (in addition to existing `.ts` for backwards compatibility during transition).
- **Blocking playlist requests**: when HLS.js requests `stream.m3u8?_HLS_msn=N&_HLS_part=M`, the server holds the response open until the playlist file on disk contains the requested media sequence / partial segment. Implementation: poll playlist mtime every 50ms with a 5-second timeout. Return 503 on timeout.
- This eliminates the polling round-trip gap — the player gets new data as soon as it's written.

### HLS.js player (embedded in `internal/hls/server.go`)

| Parameter | Current | New |
|-----------|---------|-----|
| `liveSyncDurationCount` | 1 | 1 (now 0.5s since segments halved) |
| `liveMaxLatencyDurationCount` | 3 | 2 |
| `maxLiveSyncPlaybackRate` | (unset) | 1.5 |
| `backBufferLength` | (unset) | 0 |

HLS.js LL-HLS support is already enabled via `lowLatencyMode: true`.

### Janitor (`internal/hls/janitor.go`)

- Sweep interval: 5s to 2s (keep up with shorter segment lifetime).
- Clean `.m4s` files in addition to `.ts`.

### MP4 remuxer (`internal/hls/server.go` serveMP4)

- Update the `-bsf:a aac_adtstoasc` flag — fMP4 segments already use the correct bitstream format, so this filter may need adjustment or removal depending on FFmpeg behavior with fMP4 input.

## Quality Impact

None beyond the extra keyframes. No encoder, bitrate, or resolution changes.

## Expected Latency

~1-2 seconds through a tunnel (down from ~5s).
