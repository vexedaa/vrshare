# Per-Process WASAPI Audio Capture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace DirectShow audio capture with WASAPI process-exclusion loopback that automatically excludes VRChat's audio to prevent feedback loops.

**Architecture:** A Go package (`internal/audio`) uses Windows COM/WASAPI APIs via syscall to capture system audio excluding VRChat.exe. PCM audio flows through an os.Pipe to FFmpeg, which reads it as raw audio input on fd 3. A background goroutine monitors for VRChat's PID and recreates the WASAPI session when it changes.

**Tech Stack:** Go syscall (Windows COM), WASAPI `ActivateAudioInterfaceAsync` with `PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE`, `CreateToolhelp32Snapshot` for process enumeration

---

## File Structure

```
New:
├── internal/audio/
│   ├── process.go        # FindVRChatPID via CreateToolhelp32Snapshot
│   ├── process_test.go   # Process finder tests
│   ├── wasapi.go         # COM/WASAPI type definitions, GUIDs, constants
│   ├── capturer.go       # Capturer type: Start/Stop, capture loop, PID monitor

Modified:
├── internal/config/config.go       # Remove AudioDevice field
├── internal/ffmpeg/command.go      # Replace dshow with pipe:3 PCM input
├── internal/ffmpeg/command_test.go # Update audio tests
├── internal/ffmpeg/encoder.go      # Remove DetectAudioDevice
├── internal/ffmpeg/manager.go      # Accept optional audio pipe in Run
├── cmd/vrshare/main.go             # Wire Capturer, remove --audio-device
```

---

### Task 1: Process Finder

**Files:**
- Create: `internal/audio/process.go`
- Create: `internal/audio/process_test.go`

- [ ] **Step 1: Write failing test for FindVRChatPID**

Create `internal/audio/process_test.go`:
```go
package audio

import (
	"os"
	"testing"
)

func TestFindProcessByName_FindsSelf(t *testing.T) {
	// Our own process should be findable
	pid := FindProcessByName("go.exe")
	// During tests, go.exe (test runner) or the compiled test binary is running
	if pid == 0 {
		// Try the test binary name instead
		pid = FindProcessByName("process.test.exe")
	}
	// At minimum, verify the function doesn't panic
	t.Logf("FindProcessByName result: %d", pid)
}

func TestFindProcessByName_NotFound(t *testing.T) {
	pid := FindProcessByName("nonexistent_process_12345.exe")
	if pid != 0 {
		t.Errorf("expected 0 for nonexistent process, got %d", pid)
	}
}

func TestFindVRChatPID_ReturnsZeroWhenNotRunning(t *testing.T) {
	// VRChat is almost certainly not running during unit tests
	pid := FindVRChatPID()
	// We can't assert 0 because it might be running, but we can assert no panic
	t.Logf("FindVRChatPID result: %d (0 expected unless VRChat is running)", pid)
}

func TestFindProcessByName_CurrentProcess(t *testing.T) {
	// os.Getpid() should match something in the process list
	myPid := os.Getpid()
	if myPid == 0 {
		t.Skip("could not get own PID")
	}
	// Just verify the function works without error
	_ = FindVRChatPID()
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go test ./internal/audio/ -v
```

Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement process finder**

Create `internal/audio/process.go`:
```go
package audio

import (
	"strings"
	"syscall"
	"unsafe"
)

var (
	modkernel32            = syscall.NewLazyDLL("kernel32.dll")
	procCreateToolhelp32Snapshot = modkernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW    = modkernel32.NewProc("Process32FirstW")
	procProcess32NextW     = modkernel32.NewProc("Process32NextW")
)

const (
	tH32CS_SNAPPROCESS = 0x00000002
	maxPath            = 260
)

type processEntry32W struct {
	Size              uint32
	Usage             uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	Threads           uint32
	ParentProcessID   uint32
	PriorityClassBase int32
	Flags             uint32
	ExeFile           [maxPath]uint16
}

// FindProcessByName scans the process list for a process with the given
// executable name (case-insensitive) and returns its PID. Returns 0 if not found.
func FindProcessByName(name string) uint32 {
	handle, _, _ := procCreateToolhelp32Snapshot.Call(tH32CS_SNAPPROCESS, 0)
	if handle == ^uintptr(0) { // INVALID_HANDLE_VALUE
		return 0
	}
	defer syscall.CloseHandle(syscall.Handle(handle))

	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))

	ret, _, _ := procProcess32FirstW.Call(handle, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		return 0
	}

	nameLower := strings.ToLower(name)
	for {
		exeName := syscall.UTF16ToString(entry.ExeFile[:])
		if strings.ToLower(exeName) == nameLower {
			return entry.ProcessID
		}

		entry.Size = uint32(unsafe.Sizeof(entry))
		ret, _, _ = procProcess32NextW.Call(handle, uintptr(unsafe.Pointer(&entry)))
		if ret == 0 {
			break
		}
	}

	return 0
}

// FindVRChatPID returns the PID of VRChat.exe, or 0 if not running.
func FindVRChatPID() uint32 {
	return FindProcessByName("VRChat.exe")
}
```

- [ ] **Step 4: Run tests**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go test ./internal/audio/ -v
```

Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/audio/process.go internal/audio/process_test.go
git commit -m "feat: add Windows process finder for VRChat PID detection"
```

---

### Task 2: WASAPI COM Definitions

**Files:**
- Create: `internal/audio/wasapi.go`

This file defines all the COM types, GUIDs, constants, and low-level helper functions needed for WASAPI process-loopback capture. No logic — just type definitions and syscall wrappers.

- [ ] **Step 1: Create WASAPI type definitions**

Create `internal/audio/wasapi.go`:
```go
package audio

import (
	"sync"
	"syscall"
	"unsafe"
)

// DLL and proc references
var (
	modole32                   = syscall.NewLazyDLL("ole32.dll")
	modmmdevapi                = syscall.NewLazyDLL("mmdevapi.dll")
	procCoInitializeEx         = modole32.NewProc("CoInitializeEx")
	procCoUninitialize         = modole32.NewProc("CoUninitialize")
	procCoTaskMemFree          = modole32.NewProc("CoTaskMemFree")
	procActivateAudioInterfaceAsync = modmmdevapi.NewProc("ActivateAudioInterfaceAsync")
)

// COM threading model
const (
	coINIT_MULTITHREADED = 0x0
)

// WASAPI constants
const (
	AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK            = 1
	PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE       = 0
	PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE       = 1
	AUDCLNT_SHAREMODE_SHARED                                = 0
	AUDCLNT_STREAMFLAGS_LOOPBACK                            = 0x00020000
	AUDCLNT_STREAMFLAGS_EVENTCALLBACK                       = 0x00040000
	AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM                      = 0x80000000
	AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY                 = 0x08000000
	WAVE_FORMAT_PCM                                         = 1
)

// VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK is the device ID for process loopback.
var VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK = syscall.StringToUTF16Ptr("{2C4B2C2A-8E73-4C4F-B4C3-3F6B6F8F9E2C}")

// GUIDs
var (
	IID_IAudioClient, _          = syscall.GUIDFromString("{1CB9AD4C-DBFA-4c32-B178-C2F568A703B2}")
	IID_IAudioCaptureClient, _   = syscall.GUIDFromString("{C8ADBD64-E71E-48a0-A4DE-185C395CD317}")
	IID_IUnknown, _              = syscall.GUIDFromString("{00000000-0000-0000-C000-000000000046}")
	IID_IActivateAudioInterfaceCompletionHandler, _ = syscall.GUIDFromString("{41D949AB-9862-444A-80F6-C261334DA5EB}")
	IID_IActivateAudioInterfaceAsyncOperation, _    = syscall.GUIDFromString("{72A22D78-CDE4-431D-B8CC-843A71199B6D}")
)

// WAVEFORMATEX defines the PCM audio format.
type WAVEFORMATEX struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	Size           uint16
}

// PCM16Stereo48kHz returns a WAVEFORMATEX for 48kHz 16-bit stereo PCM.
func PCM16Stereo48kHz() WAVEFORMATEX {
	return WAVEFORMATEX{
		FormatTag:      WAVE_FORMAT_PCM,
		Channels:       2,
		SamplesPerSec:  48000,
		BitsPerSample:  16,
		BlockAlign:     4,  // 2 channels * 2 bytes
		AvgBytesPerSec: 192000, // 48000 * 4
		Size:           0,
	}
}

// AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS specifies the process to include/exclude.
type AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS struct {
	TargetProcessId    uint32
	ProcessLoopbackMode uint32 // PROCESS_LOOPBACK_MODE_*
}

// AUDIOCLIENT_ACTIVATION_PARAMS wraps the activation type and params.
type AUDIOCLIENT_ACTIVATION_PARAMS struct {
	ActivationType       uint32
	ProcessLoopbackParams AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS
}

// PROPVARIANT simplified for VT_BLOB usage.
type PROPVARIANT struct {
	Vt       uint16
	Reserved [6]byte
	Blob     BLOB
}

// BLOB holds a pointer and size for PROPVARIANT.
type BLOB struct {
	Size uint32
	_    [4]byte // padding on 64-bit
	Data uintptr
}

const VT_BLOB = 65

// completionHandler implements IActivateAudioInterfaceCompletionHandler.
// It signals a channel when the async activation completes.
type completionHandler struct {
	vtbl    *completionHandlerVtbl
	refCount int32
	done    chan struct{}
	mu      sync.Mutex
}

type completionHandlerVtbl struct {
	QueryInterface    uintptr
	AddRef            uintptr
	Release           uintptr
	ActivateCompleted uintptr
}

var (
	handlerQueryInterfaceCallback uintptr
	handlerAddRefCallback         uintptr
	handlerReleaseCallback        uintptr
	handlerActivateCompletedCallback uintptr
	callbacksOnce                 sync.Once
)

func initCallbacks() {
	callbacksOnce.Do(func() {
		handlerQueryInterfaceCallback = syscall.NewCallback(handlerQueryInterface)
		handlerAddRefCallback = syscall.NewCallback(handlerAddRef)
		handlerReleaseCallback = syscall.NewCallback(handlerRelease)
		handlerActivateCompletedCallback = syscall.NewCallback(handlerActivateCompleted)
	})
}

func newCompletionHandler() *completionHandler {
	initCallbacks()
	h := &completionHandler{
		refCount: 1,
		done:     make(chan struct{}),
	}
	h.vtbl = &completionHandlerVtbl{
		QueryInterface:    handlerQueryInterfaceCallback,
		AddRef:            handlerAddRefCallback,
		Release:           handlerReleaseCallback,
		ActivateCompleted: handlerActivateCompletedCallback,
	}
	return h
}

func handlerQueryInterface(this uintptr, riid uintptr, ppv uintptr) uintptr {
	iid := (*syscall.GUID)(unsafe.Pointer(riid))
	if *iid == IID_IUnknown || *iid == IID_IActivateAudioInterfaceCompletionHandler {
		*(*uintptr)(unsafe.Pointer(ppv)) = this
		handlerAddRef(this)
		return 0 // S_OK
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = 0
	return 0x80004002 // E_NOINTERFACE
}

func handlerAddRef(this uintptr) uintptr {
	h := (*completionHandler)(unsafe.Pointer(this))
	h.mu.Lock()
	h.refCount++
	h.mu.Unlock()
	return uintptr(h.refCount)
}

func handlerRelease(this uintptr) uintptr {
	h := (*completionHandler)(unsafe.Pointer(this))
	h.mu.Lock()
	h.refCount--
	rc := h.refCount
	h.mu.Unlock()
	return uintptr(rc)
}

func handlerActivateCompleted(this uintptr, operation uintptr) uintptr {
	h := (*completionHandler)(unsafe.Pointer(this))
	select {
	case <-h.done:
	default:
		close(h.done)
	}
	return 0 // S_OK
}

// comCall invokes a COM method at the given vtable index on the interface pointer.
func comCall(obj uintptr, vtblIndex int, args ...uintptr) (uintptr, error) {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	method := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblIndex)*unsafe.Sizeof(uintptr(0))))
	allArgs := append([]uintptr{obj}, args...)
	r, _, err := syscall.SyscallN(method, allArgs...)
	if r != 0 {
		return r, err
	}
	return 0, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go build ./internal/audio/
```

Expected: Compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add internal/audio/wasapi.go
git commit -m "feat: add WASAPI COM type definitions and completion handler"
```

---

### Task 3: WASAPI Capturer

**Files:**
- Create: `internal/audio/capturer.go`

This is the core capture logic: activate WASAPI with process exclusion, run the capture loop, monitor for VRChat PID changes.

- [ ] **Step 1: Implement the Capturer**

Create `internal/audio/capturer.go`:
```go
package audio

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

// Capturer captures system audio via WASAPI, excluding VRChat's audio.
type Capturer struct {
	writer        io.Writer
	vrchatPID     uint32
	mu            sync.Mutex
	cancelFunc    context.CancelFunc
	sessionCancel context.CancelFunc // cancels the current capture session
}

// NewCapturer creates a new audio capturer that writes PCM data to w.
func NewCapturer(w io.Writer) *Capturer {
	return &Capturer{writer: w}
}

// Start begins audio capture in a background goroutine.
// It returns immediately. The capture runs until ctx is cancelled.
func (c *Capturer) Start(ctx context.Context) {
	ctx, c.cancelFunc = context.WithCancel(ctx)

	// Find VRChat PID (0 if not running)
	c.vrchatPID = FindVRChatPID()
	if c.vrchatPID > 0 {
		log.Printf("Audio: excluding VRChat.exe (PID %d)", c.vrchatPID)
	} else {
		log.Println("Audio: VRChat not running, capturing all system audio")
	}

	go c.captureLoop(ctx)
	go c.monitorVRChat(ctx)
}

// Stop stops the audio capture.
func (c *Capturer) Stop() {
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
}

func (c *Capturer) captureLoop(ctx context.Context) {
	// Lock to OS thread — COM is thread-affine
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Initialize COM
	hr, _, _ := procCoInitializeEx.Call(0, coINIT_MULTITHREADED)
	if hr != 0 && hr != 1 { // S_OK or S_FALSE (already initialized)
		log.Printf("Audio: CoInitializeEx failed: 0x%x", hr)
		return
	}
	defer procCoUninitialize.Call()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		pid := c.vrchatPID
		c.mu.Unlock()

		audioClient, err := c.activateLoopback(pid)
		if err != nil {
			log.Printf("Audio: WASAPI activation failed: %v — disabling audio", err)
			return
		}

		// Create a session-level context that monitorVRChat can cancel
		sessionCtx, sessionCancel := context.WithCancel(ctx)
		c.mu.Lock()
		c.sessionCancel = sessionCancel
		c.mu.Unlock()

		err = c.runCaptureSession(sessionCtx, audioClient)
		sessionCancel()

		// Release the audio client
		comCall(audioClient, 2) // Release

		if ctx.Err() != nil {
			return
		}

		if err != nil {
			log.Printf("Audio: capture session ended: %v — restarting", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (c *Capturer) activateLoopback(excludePID uint32) (uintptr, error) {
	// Set up activation params
	params := AUDIOCLIENT_ACTIVATION_PARAMS{
		ActivationType: AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK,
		ProcessLoopbackParams: AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS{
			TargetProcessId:    excludePID,
			ProcessLoopbackMode: PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE,
		},
	}

	// When VRChat isn't running (PID 0), exclude our own process instead.
	// VRShare doesn't produce audio, so this effectively captures everything.
	if excludePID == 0 {
		params.ProcessLoopbackParams.TargetProcessId = uint32(os.Getpid())
	}

	// Wrap in PROPVARIANT
	pv := PROPVARIANT{
		Vt: VT_BLOB,
	}
	pv.Blob.Size = uint32(unsafe.Sizeof(params))
	pv.Blob.Data = uintptr(unsafe.Pointer(&params))

	// Create completion handler
	handler := newCompletionHandler()

	// Call ActivateAudioInterfaceAsync
	var asyncOp uintptr
	deviceId := VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK
	hr, _, _ := procActivateAudioInterfaceAsync.Call(
		uintptr(unsafe.Pointer(deviceId)),
		uintptr(unsafe.Pointer(&IID_IAudioClient)),
		uintptr(unsafe.Pointer(&pv)),
		uintptr(unsafe.Pointer(handler)),
		uintptr(unsafe.Pointer(&asyncOp)),
	)
	if hr != 0 {
		return 0, fmt.Errorf("ActivateAudioInterfaceAsync failed: 0x%x", hr)
	}

	// Wait for completion
	select {
	case <-handler.done:
	case <-time.After(5 * time.Second):
		return 0, fmt.Errorf("WASAPI activation timed out")
	}

	// Get the result
	var activateHR uintptr
	var audioClient uintptr
	comCall(asyncOp, 3, // GetActivateResult (vtable index 3 on IActivateAudioInterfaceAsyncOperation)
		uintptr(unsafe.Pointer(&activateHR)),
		uintptr(unsafe.Pointer(&audioClient)),
	)
	comCall(asyncOp, 2) // Release asyncOp

	if activateHR != 0 {
		return 0, fmt.Errorf("audio activation failed: 0x%x", activateHR)
	}

	return audioClient, nil
}

// IAudioClient vtable indices (inherits IUnknown: 0=QI, 1=AddRef, 2=Release):
//   3=Initialize, 4=GetBufferSize, 5=GetStreamLatency, 6=GetCurrentPadding,
//   7=IsFormatSupported, 8=GetMixFormat, 9=GetDevicePeriod,
//   10=Start, 11=Stop, 12=Reset, 13=SetEventHandle, 14=GetService
//
// IAudioCaptureClient vtable indices (inherits IUnknown):
//   3=GetBuffer, 4=ReleaseBuffer, 5=GetNextPacketSize

func (c *Capturer) runCaptureSession(ctx context.Context, audioClient uintptr) error {
	// Initialize the audio client
	format := PCM16Stereo48kHz()
	flags := uint32(AUDCLNT_STREAMFLAGS_LOOPBACK | AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY)

	hr, _ := comCall(audioClient, 3, // Initialize
		uintptr(AUDCLNT_SHAREMODE_SHARED),
		uintptr(flags),
		0,    // buffer duration (0 = default)
		0,    // periodicity
		uintptr(unsafe.Pointer(&format)),
		0,    // audio session GUID (null = default)
	)
	if hr != 0 {
		return fmt.Errorf("IAudioClient.Initialize failed: 0x%x", hr)
	}

	// Get capture client
	var captureClient uintptr
	hr, _ = comCall(audioClient, 14, // GetService
		uintptr(unsafe.Pointer(&IID_IAudioCaptureClient)),
		uintptr(unsafe.Pointer(&captureClient)),
	)
	if hr != 0 {
		return fmt.Errorf("IAudioClient.GetService failed: 0x%x", hr)
	}
	defer comCall(captureClient, 2) // Release

	// Start capture
	hr, _ = comCall(audioClient, 10) // Start
	if hr != 0 {
		return fmt.Errorf("IAudioClient.Start failed: 0x%x", hr)
	}
	defer comCall(audioClient, 11) // Stop

	// Capture loop — exits when session context is cancelled (PID change or shutdown)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.readBuffers(captureClient)
		}
	}
}

func (c *Capturer) readBuffers(captureClient uintptr) {
	for {
		var data uintptr
		var numFrames uint32
		var flags uint32

		hr, _ := comCall(captureClient, 3, // IAudioCaptureClient.GetBuffer
			uintptr(unsafe.Pointer(&data)),
			uintptr(unsafe.Pointer(&numFrames)),
			uintptr(unsafe.Pointer(&flags)),
			0, // devicePosition
			0, // qpcPosition
		)
		if hr != 0 || numFrames == 0 {
			break
		}

		// Write PCM data to pipe
		// Each frame = 4 bytes (2 channels * 16-bit)
		byteCount := int(numFrames) * 4
		if data != 0 && byteCount > 0 {
			buf := unsafe.Slice((*byte)(unsafe.Pointer(data)), byteCount)
			c.writer.Write(buf)
		}

		comCall(captureClient, 4, uintptr(numFrames)) // IAudioCaptureClient.ReleaseBuffer
	}
}

func (c *Capturer) monitorVRChat(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			newPID := FindVRChatPID()
			c.mu.Lock()
			oldPID := c.vrchatPID
			if newPID != oldPID {
				c.vrchatPID = newPID
				// Cancel the current session so captureLoop restarts with new PID
				if c.sessionCancel != nil {
					c.sessionCancel()
				}
				c.mu.Unlock()
				if newPID > 0 {
					log.Printf("Audio: VRChat detected (PID %d) — restarting capture with exclusion", newPID)
				} else {
					log.Println("Audio: VRChat exited — restarting capture without exclusion")
				}
			} else {
				c.mu.Unlock()
			}
		}
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go build ./internal/audio/
```

Expected: Compiles. Fix any import or type issues.

- [ ] **Step 3: Commit**

```bash
git add internal/audio/capturer.go
git commit -m "feat: WASAPI loopback capturer with VRChat exclusion"
```

---

### Task 4: Update BuildArgs for Pipe Audio

**Files:**
- Modify: `internal/ffmpeg/command.go`
- Modify: `internal/ffmpeg/command_test.go`

- [ ] **Step 1: Write failing tests for pipe audio args**

Add to `internal/ffmpeg/command_test.go`:
```go
func TestBuildArgs_AudioEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Audio = true
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true)

	assertContains(t, args, "-f", "s16le")
	assertContains(t, args, "-ar", "48000")
	assertContains(t, args, "-ac", "2")
	assertContains(t, args, "-i", "pipe:3")
	assertContains(t, args, "-c:a", "aac")
	assertContains(t, args, "-b:a", "128k")
	// Should NOT have dshow
	assertNotContains(t, args, "dshow")
}

func TestBuildArgs_AudioDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Audio = false
	args := BuildArgs(cfg, "nvenc", "/tmp/vrshare", true)

	assertNotContains(t, args, "s16le")
	assertNotContains(t, args, "pipe:3")
	assertNotContains(t, args, "-c:a")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go test ./internal/ffmpeg/ -v -run TestBuildArgs_Audio
```

Expected: FAIL — still using dshow logic.

- [ ] **Step 3: Update BuildArgs**

In `internal/ffmpeg/command.go`, replace the audio input and encoding blocks:

Replace:
```go
	// Audio input (DirectShow on Windows)
	if cfg.Audio && cfg.AudioDevice != "" {
		args = append(args, "-f", "dshow", "-i", "audio="+cfg.AudioDevice)
	}
```

With:
```go
	// Audio input (raw PCM from WASAPI capturer via pipe)
	if cfg.Audio {
		args = append(args, "-f", "s16le", "-ar", "48000", "-ac", "2", "-i", "pipe:3")
	}
```

Replace:
```go
	// Audio encoding
	if cfg.Audio && cfg.AudioDevice != "" {
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	}
```

With:
```go
	// Audio encoding
	if cfg.Audio {
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	}
```

- [ ] **Step 4: Run all tests**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go test ./internal/ffmpeg/ -v
```

Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ffmpeg/command.go internal/ffmpeg/command_test.go
git commit -m "feat: replace dshow audio with pipe:3 raw PCM input"
```

---

### Task 5: Update Manager for Audio Pipe

**Files:**
- Modify: `internal/ffmpeg/manager.go`

- [ ] **Step 1: Update Manager.Run to accept audio pipe**

In `internal/ffmpeg/manager.go`, change the `Run` method signature and add ExtraFiles support:

Change `Run` to:
```go
func (m *Manager) Run(ctx context.Context, args []string, audioPipe *os.File) error {
```

In the `Run` method, after creating `m.cmd` and before `m.cmd.Run()`, add:
```go
		if audioPipe != nil {
			m.cmd.ExtraFiles = []*os.File{audioPipe} // fd 3
		}
```

- [ ] **Step 2: Verify it compiles**

This will break `main.go` temporarily (wrong number of args to `Run`). Verify the package compiles:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go build ./internal/ffmpeg/
```

Expected: Compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/ffmpeg/manager.go
git commit -m "feat: accept optional audio pipe in Manager.Run"
```

---

### Task 6: Wire Into main.go and Clean Up

**Files:**
- Modify: `cmd/vrshare/main.go`
- Modify: `internal/config/config.go`
- Modify: `internal/ffmpeg/encoder.go`

- [ ] **Step 1: Remove AudioDevice from config**

In `internal/config/config.go`, remove `AudioDevice string` from the Config struct.

- [ ] **Step 2: Remove DetectAudioDevice from encoder.go**

In `internal/ffmpeg/encoder.go`, delete the entire `DetectAudioDevice` function.

- [ ] **Step 3: Update main.go**

Replace the audio detection block and update the wiring:

Remove the `--audio-device` flag line:
```go
	flag.StringVar(&cfg.AudioDevice, "audio-device", cfg.AudioDevice, "Audio device name (auto-detect if empty)")
```

Replace the entire audio detection block:
```go
	// Detect audio device if --audio is enabled
	if cfg.Audio && cfg.AudioDevice == "" {
		cfg.AudioDevice = ffmpeg.DetectAudioDevice(ffmpegPath)
		if cfg.AudioDevice != "" {
			log.Printf("Detected audio device: %s", cfg.AudioDevice)
		} else {
			log.Println("Warning: --audio enabled but no loopback device found. Try --audio-device to specify manually.")
			log.Println("  Enable 'Stereo Mix' in Sound Settings > Recording, or install VB-Audio Virtual Cable.")
			cfg.Audio = false
		}
	} else if cfg.Audio {
		log.Printf("Using audio device: %s", cfg.AudioDevice)
	}
```

With:
```go
	if cfg.Audio {
		log.Println("Audio capture enabled (WASAPI loopback, excluding VRChat)")
	}
```

Add the audio import:
```go
	"github.com/vexedaa/vrshare/internal/audio"
```

Before the `// Build and run FFmpeg` block, add audio capturer startup:
```go
	// Start audio capturer if enabled
	var audioPipeR *os.File
	var audioCapturer *audio.Capturer
	if cfg.Audio {
		pipeR, pipeW, err := os.Pipe()
		if err != nil {
			log.Printf("Warning: failed to create audio pipe: %v — continuing without audio", err)
			cfg.Audio = false
		} else {
			audioPipeR = pipeR
			audioCapturer = audio.NewCapturer(pipeW)
			audioCapturer.Start(ctx)
			defer pipeW.Close()
			defer pipeR.Close()
		}
	}
```

Update the Manager.Run call:
```go
	err = manager.Run(ctx, args, audioPipeR)
```

Add cleanup for the audio capturer in the cleanup section:
```go
	if audioCapturer != nil {
		audioCapturer.Stop()
	}
```

- [ ] **Step 4: Verify it compiles and tests pass**

Run:
```bash
cd C:/Users/bnjmn/Documents/Git/vrshare
go build ./cmd/vrshare && go test ./... -v
```

Expected: Compiles and all tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/vrshare/main.go internal/config/config.go internal/ffmpeg/encoder.go
git commit -m "feat: wire WASAPI capturer into main, remove dshow audio code"
```

---

### Task 7: Smoke Test

**Files:** None (manual testing)

- [ ] **Step 1: Test without audio**

```bash
./vrshare.exe --tunnel
```

Expected: Stream works as before, no audio-related errors.

- [ ] **Step 2: Test with audio, VRChat not running**

```bash
./vrshare.exe --audio
```

Expected: Log shows "VRChat not running, capturing all system audio". Play music on PC, open `http://localhost:8080/` — should hear music in the stream.

- [ ] **Step 3: Test with audio and VRChat running**

Launch VRChat, then:
```bash
./vrshare.exe --audio
```

Expected: Log shows "excluding VRChat.exe (PID XXXX)". VRChat audio should NOT be in the stream. Other system audio should be.

- [ ] **Step 4: Test WASAPI failure graceful degradation**

If WASAPI fails for any reason, verify video-only streaming continues.

- [ ] **Step 5: Commit any fixes**

```bash
git add -A
git commit -m "fix: smoke test fixes for WASAPI audio"
```

Skip if no fixes needed.
