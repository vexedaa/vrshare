# Per-Process Audio Capture Design Spec

## Purpose

Replace VRShare's DirectShow audio capture with WASAPI process-exclusion loopback. This captures all system audio output except VRChat's audio, preventing feedback loops when VRChat plays back the stream.

## Requirements

- Capture system audio output excluding VRChat.exe's process tree
- Use Windows WASAPI `PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE` API (Windows 10 2004+)
- Pure Go implementation via syscall (no cgo, no external binaries)
- If VRChat isn't running at start, capture all audio and dynamically exclude when VRChat launches
- Session recreation on VRChat PID change causes brief (~50-100ms) audio gap — acceptable
- Graceful degradation: if WASAPI init fails, disable audio and continue video-only
- Replace existing DirectShow (`-f dshow`) audio path entirely — no fallback
- Remove `--audio-device` flag (no longer needed)

## Architecture

### New Package: `internal/audio`

Single main type: `Capturer`

```
Capturer
  ├── FindVRChatPID() → scans for VRChat.exe, returns PID or 0
  ├── Start(ctx, io.Writer) → starts capture goroutine
  ├── capture loop (locked OS thread)
  │   ├── CoInitializeEx
  │   ├── ActivateAudioInterfaceAsync (with process exclusion)
  │   ├── IAudioClient3.Initialize (shared mode, 48kHz, 16-bit stereo)
  │   ├── IAudioClient3.Start
  │   ├── loop: GetBuffer → write PCM to io.Writer → ReleaseBuffer
  │   └── monitor VRChat PID every 5s → recreate session if changed
  └── Stop() → cancels context, cleanup
```

### Data Flow

```
System Audio (all apps except VRChat.exe)
    |
    v
WASAPI Loopback Capture (Go goroutine, locked OS thread)
    |  raw PCM: s16le, 48kHz, stereo
    v
os.Pipe (write end)
    |
    v
FFmpeg (reads pipe:3 as raw PCM input)
    |  encodes to AAC 128kbps
    v
HLS segments (.ts with audio + video)
```

### COM API Surface

Called from Go via `syscall.SyscallN` on COM vtable pointers:

| API | Purpose |
|-----|---------|
| `CoInitializeEx` | Init COM on capture thread |
| `ActivateAudioInterfaceAsync` | Create loopback client with process exclusion |
| `IActivateAudioInterfaceAsyncOperation.GetActivateResult` | Get result of async activation |
| `IAudioClient3.Initialize` | Configure session (shared, 48kHz, s16le, stereo) |
| `IAudioClient3.GetBufferSize` | Get buffer size for timing |
| `IAudioClient3.GetService` | Get IAudioCaptureClient |
| `IAudioClient3.Start` / `Stop` | Start/stop capture |
| `IAudioCaptureClient.GetBuffer` | Read PCM frames |
| `IAudioCaptureClient.ReleaseBuffer` | Release read frames |

Required GUIDs: `IID_IAudioClient3`, `IID_IAudioCaptureClient`, `CLSID_AudioClient`.

Activation params struct: `AUDIOCLIENT_ACTIVATION_PARAMS` with `AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK` and `PROCESS_LOOPBACK_PARAMS` containing the target PID and `PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE`.

### FFmpeg Integration

`BuildArgs` when `cfg.Audio == true`:
- Add: `-f s16le -ar 48000 -ac 2 -i pipe:3`
- Add: `-c:a aac -b:a 128k`
- Remove: all `-f dshow -i audio=...` code

`Manager.Run` changes:
- Accept an optional `*os.File` (audio pipe read end)
- Attach to `exec.Cmd.ExtraFiles` (maps to fd 3 in child process)

`main.go` changes:
- When `--audio` is set: create `os.Pipe()`, start `audio.Capturer` writing to pipe write end, pass pipe read end to Manager
- Remove `--audio-device` flag
- Remove `DetectAudioDevice` call

### VRChat PID Monitoring

- On start: scan process list for `VRChat.exe`, store PID (0 if not found)
- Every 5 seconds: re-scan for `VRChat.exe`
- If PID changed (including 0→found or found→gone):
  - Stop current WASAPI session
  - Create new session with updated exclusion list
  - Brief audio gap during recreation
  - Log the event

Process scanning uses `CreateToolhelp32Snapshot` + `Process32First/Next` via syscall.

### Error Handling

| Scenario | Behavior |
|----------|----------|
| COM init fails | Log error, disable audio, continue video-only |
| WASAPI activation fails | Log error, disable audio, continue video-only |
| VRChat not running | Capture all audio (no exclusion), monitor for VRChat launch |
| VRChat launches mid-stream | Recreate session with exclusion, brief audio gap |
| VRChat exits mid-stream | Recreate session without exclusion, brief audio gap |
| Pipe write fails | Log error, stop audio capture |
| Windows < 10 2004 | WASAPI activation fails, graceful degradation to no audio |

## Files Changed

```
New:
├── internal/audio/
│   ├── capturer.go       # Capturer type, Start/Stop, PID monitoring
│   ├── wasapi.go         # COM/WASAPI syscall wrappers, GUIDs, structs
│   ├── process.go        # FindVRChatPID via CreateToolhelp32Snapshot
│   └── capturer_test.go  # PID finder tests, BuildArgs audio tests

Modified:
├── internal/config/config.go       # Remove AudioDevice field
├── internal/ffmpeg/command.go      # Replace dshow with pipe:3 PCM input
├── internal/ffmpeg/command_test.go # Update audio tests
├── internal/ffmpeg/encoder.go      # Remove DetectAudioDevice
├── internal/ffmpeg/manager.go      # Accept optional audio pipe in Run
├── cmd/vrshare/main.go             # Wire Capturer, remove --audio-device
```

## Testing

### Unit Tests
- `BuildArgs` with `Audio=true`: verify `-f s16le -ar 48000 -ac 2 -i pipe:3` and `-c:a aac -b:a 128k`
- `FindVRChatPID`: verify process scanning (can test with current process as proxy)
- COM struct layouts: verify struct sizes match Windows API expectations

### Manual / Smoke Tests
- Start VRShare with `--audio`, play music, verify audio in HLS stream
- Start VRShare with `--audio` before VRChat, launch VRChat, verify exclusion kicks in
- Start VRShare with `--audio` after VRChat, verify immediate exclusion
- Verify video-only continues if WASAPI fails
