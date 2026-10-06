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

const (
	// WASAPI buffer flags
	AUDCLNT_BUFFERFLAGS_DATA_DISCONTINUITY = 0x1
	AUDCLNT_BUFFERFLAGS_SILENT             = 0x2

	// PCM format: 2 channels, 16-bit, 48kHz = 4 bytes per frame
	bytesPerFrame = 4
	sampleRate    = 48000
)

// Capturer captures audio via WASAPI: by default all system audio except
// VRChat's (process loopback), or everything playing on one chosen output
// device (endpoint loopback).
type Capturer struct {
	writer        io.Writer
	vrchatPID     uint32
	device        string // "" = all system audio except VRChat; else an output device ID or name
	mu            sync.Mutex
	cancelFunc    context.CancelFunc
	sessionCancel context.CancelFunc
	silenceBuf    []byte // pre-allocated silence buffer
}

// NewCapturer creates a new audio capturer that writes PCM data to w.
func NewCapturer(w io.Writer) *Capturer {
	// Pre-allocate a silence buffer (10ms worth of silence)
	silenceFrames := sampleRate / 100 // 480 frames = 10ms
	return &Capturer{
		writer:     w,
		silenceBuf: make([]byte, silenceFrames*bytesPerFrame),
	}
}

// Start begins audio capture in a background goroutine.
// It returns immediately. The capture runs until ctx is cancelled.
func (c *Capturer) Start(ctx context.Context) {
	ctx, c.cancelFunc = context.WithCancel(ctx)

	c.vrchatPID = FindVRChatPID()
	c.mu.Lock()
	systemAudio := IsSystemAudio(c.device)
	c.mu.Unlock()
	// captureLoop announces the source once it opens.
	if systemAudio && c.vrchatPID > 0 {
		log.Printf("Audio: excluding VRChat.exe (PID %d)", c.vrchatPID)
	}

	go c.captureLoop(ctx)
	go c.monitorVRChat(ctx)
}

// SetDevice selects what to capture: "" (or the legacy "Default Output
// Device") for all system audio except VRChat, or an output device's endpoint
// ID or friendly name. Takes effect immediately on a running capture.
func (c *Capturer) SetDevice(device string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if device == c.device {
		return
	}
	c.device = device
	if c.sessionCancel != nil {
		c.sessionCancel()
	}
}

// Stop stops the audio capture.
func (c *Capturer) Stop() {
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
}

func (c *Capturer) captureLoop(ctx context.Context) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Initialize WinRT (required for ActivateAudioInterfaceAsync)
	hr, _, _ := procRoInitialize.Call(1) // RO_INIT_MULTITHREADED
	if hr != 0 && hr != 1 {
		log.Printf("Audio: RoInitialize failed: 0x%x, trying CoInitializeEx", hr)
		hr, _, _ = procCoInitializeEx.Call(0, coINIT_MULTITHREADED)
		if hr != 0 && hr != 1 {
			log.Printf("Audio: COM initialization failed (0x%x) — audio capture disabled", hr)
			return
		}
		defer procCoUninitialize.Call()
	} else {
		defer procRoUninitialize.Call()
	}

	lastErr := ""    // last failure logged, so a persistent one isn't logged every retry
	lastSource := "" // last source announced, so restarts don't repeat it
	for {
		if ctx.Err() != nil {
			return
		}

		// Read the source and publish this session's cancel func under one lock,
		// so a SetDevice or VRChat change from here on always cancels this
		// session — including while it is still activating or waiting to retry.
		sessionCtx, sessionCancel := context.WithCancel(ctx)
		c.mu.Lock()
		pid := c.vrchatPID
		device := c.device
		c.sessionCancel = sessionCancel
		c.mu.Unlock()

		var (
			audioClient   uintptr
			source        = "all system audio except VRChat"
			stopKeepAlive = func() {}
			bufDuration   int64
			err           error
		)
		if IsSystemAudio(device) {
			audioClient, err = c.activateLoopback(pid)
		} else {
			// A missing device is retried below with silence, never swapped for
			// system audio: the user picked a device to leave other sound out.
			var name string
			audioClient, name, stopKeepAlive, err = activateEndpointLoopback(device)
			source = fmt.Sprintf("output device %q", name)
			bufDuration = endpointBufferDuration
		}
		if err != nil {
			if msg := err.Error(); msg != lastErr {
				log.Printf("Audio: capture unavailable: %v — sending silence and retrying", err)
				lastErr = msg
			}
			lastSource = ""
			// Keep FFmpeg's audio input fed while we wait to retry. A source
			// change ends the wait early.
			wErr := c.padSilence(sessionCtx, 1*time.Second)
			sessionCancel()
			if wErr != nil {
				log.Printf("Audio: write error: %v — stopping capture", wErr)
				return
			}
			continue
		}
		if source != lastSource {
			log.Printf("Audio: capturing %s", source)
			lastSource = source
		}

		err = c.runCaptureSession(sessionCtx, audioClient, bufDuration)
		sessionCancel()

		comCall(audioClient, 2) // Release
		stopKeepAlive()

		if ctx.Err() != nil {
			return
		}

		if err == nil {
			lastErr = ""
		} else if msg := err.Error(); msg != lastErr {
			log.Printf("Audio: capture session ended: %v", err)
			lastErr = msg
		}

		// Fill the gap before the next session to keep audio continuous
		if wErr := c.padSilence(ctx, 100*time.Millisecond); wErr != nil {
			log.Printf("Audio: write error: %v — stopping capture", wErr)
			return
		}
	}
}

// padSilence feeds silence to the writer in real time for d, or until ctx is
// done, so FFmpeg's audio input never starves while capture is unavailable.
// Pacing matters: writing the whole duration up front puts audio that far
// ahead of the wall clock, and once capture resumes the excess becomes a
// permanent delay of audio behind video.
func (c *Capturer) padSilence(ctx context.Context, d time.Duration) error {
	total := int(d.Seconds() * sampleRate) // frames
	written := 0
	start := time.Now()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		due := int(time.Since(start).Seconds() * sampleRate)
		if due > total {
			due = total
		}
		for written < due {
			n := due - written
			if max := len(c.silenceBuf) / bytesPerFrame; n > max {
				n = max
			}
			if _, err := c.writer.Write(c.silenceBuf[:n*bytesPerFrame]); err != nil {
				return err
			}
			written += n
		}
		if written >= total {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (c *Capturer) activateLoopback(excludePID uint32) (uintptr, error) {
	params := AUDIOCLIENT_ACTIVATION_PARAMS{
		ActivationType: AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK,
		ProcessLoopbackParams: AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS{
			TargetProcessId:     excludePID,
			ProcessLoopbackMode: PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE,
		},
	}

	// When VRChat isn't running (PID 0), exclude our own process instead.
	// VRShare doesn't produce audio, so this effectively captures everything.
	if excludePID == 0 {
		params.ProcessLoopbackParams.TargetProcessId = uint32(os.Getpid())
	}

	pv := PROPVARIANT{Vt: VT_BLOB}
	pv.Blob.Size = uint32(unsafe.Sizeof(params))
	pv.Blob.Data = uintptr(unsafe.Pointer(&params))

	handler := newCompletionHandler()

	var asyncOp uintptr
	hr, _, _ := procActivateAudioInterfaceAsync.Call(
		uintptr(unsafe.Pointer(virtualAudioDeviceProcessLoopback)),
		uintptr(unsafe.Pointer(&IID_IAudioClient)),
		uintptr(unsafe.Pointer(&pv)),
		uintptr(unsafe.Pointer(handler)),
		uintptr(unsafe.Pointer(&asyncOp)),
	)
	if hr != 0 {
		return 0, fmt.Errorf("ActivateAudioInterfaceAsync failed: 0x%x", hr)
	}

	select {
	case <-handler.done:
	case <-time.After(5 * time.Second):
		return 0, fmt.Errorf("WASAPI activation timed out")
	}

	var activateHR uintptr
	var audioClient uintptr
	// IActivateAudioInterfaceAsyncOperation vtable: 0=QI, 1=AddRef, 2=Release, 3=GetActivateResult
	comCall(asyncOp, 3,
		uintptr(unsafe.Pointer(&activateHR)),
		uintptr(unsafe.Pointer(&audioClient)),
	)
	comCall(asyncOp, 2) // Release

	if activateHR != 0 {
		return 0, fmt.Errorf("audio activation failed: 0x%x", activateHR)
	}

	return audioClient, nil
}

// IAudioClient vtable indices (inherits IUnknown: 0=QI, 1=AddRef, 2=Release):
//
//	3=Initialize, 4=GetBufferSize, 5=GetStreamLatency, 6=GetCurrentPadding,
//	7=IsFormatSupported, 8=GetMixFormat, 9=GetDevicePeriod,
//	10=Start, 11=Stop, 12=Reset, 13=SetEventHandle, 14=GetService
//
// IAudioCaptureClient vtable (inherits IUnknown):
//
//	3=GetBuffer, 4=ReleaseBuffer, 5=GetNextPacketSize
//
// bufDuration is the stream buffer in 100ns units (0 = engine default).
func (c *Capturer) runCaptureSession(ctx context.Context, audioClient uintptr, bufDuration int64) error {
	format := PCM16Stereo48kHz()
	flags := uint32(AUDCLNT_STREAMFLAGS_LOOPBACK | AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY)

	hr, _ := comCall(audioClient, 3, // Initialize
		uintptr(AUDCLNT_SHAREMODE_SHARED),
		uintptr(flags),
		uintptr(bufDuration),
		0, // periodicity
		uintptr(unsafe.Pointer(&format)),
		0, // session GUID
	)
	if hr != 0 {
		return fmt.Errorf("IAudioClient.Initialize failed: 0x%x", hr)
	}

	var captureClient uintptr
	hr, _ = comCall(audioClient, 14, // GetService
		uintptr(unsafe.Pointer(&IID_IAudioCaptureClient)),
		uintptr(unsafe.Pointer(&captureClient)),
	)
	if hr != 0 {
		return fmt.Errorf("IAudioClient.GetService failed: 0x%x", hr)
	}
	defer comCall(captureClient, 2) // Release

	hr, _ = comCall(audioClient, 10) // Start
	if hr != 0 {
		return fmt.Errorf("IAudioClient.Start failed: 0x%x", hr)
	}
	defer comCall(audioClient, 11) // Stop

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.readBuffers(captureClient); err != nil {
				return err
			}
		}
	}
}

func (c *Capturer) readBuffers(captureClient uintptr) error {
	for {
		var data uintptr
		var numFrames uint32
		var flags uint32

		hr, _ := comCall(captureClient, 3, // GetBuffer
			uintptr(unsafe.Pointer(&data)),
			uintptr(unsafe.Pointer(&numFrames)),
			uintptr(unsafe.Pointer(&flags)),
			0, // devicePosition
			0, // qpcPosition
		)
		if hr&0x80000000 != 0 {
			// A real failure, e.g. AUDCLNT_E_DEVICE_INVALIDATED when the device
			// is unplugged. End the session so the loop retries with silence.
			return fmt.Errorf("IAudioCaptureClient.GetBuffer failed: 0x%x", hr)
		}
		if numFrames == 0 { // AUDCLNT_S_BUFFER_EMPTY: no data yet
			break
		}

		byteCount := int(numFrames) * bytesPerFrame
		var writeErr error

		if flags&AUDCLNT_BUFFERFLAGS_SILENT != 0 {
			// Buffer is silent — write zeroes instead of potentially garbage data
			remaining := byteCount
			for remaining > 0 {
				n := len(c.silenceBuf)
				if n > remaining {
					n = remaining
				}
				if _, err := c.writer.Write(c.silenceBuf[:n]); err != nil {
					writeErr = err
					break
				}
				remaining -= n
			}
		} else if data != 0 && byteCount > 0 {
			buf := unsafe.Slice((*byte)(unsafe.Pointer(data)), byteCount)
			_, writeErr = c.writer.Write(buf)
		}

		comCall(captureClient, 4, uintptr(numFrames)) // ReleaseBuffer

		if writeErr != nil {
			return writeErr
		}
	}
	return nil
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
				// Only system-audio capture excludes VRChat; a single-device
				// capture doesn't need restarting.
				if c.sessionCancel != nil && IsSystemAudio(c.device) {
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
