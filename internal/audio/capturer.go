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
	sessionCancel context.CancelFunc
}

// NewCapturer creates a new audio capturer that writes PCM data to w.
func NewCapturer(w io.Writer) *Capturer {
	return &Capturer{writer: w}
}

// Start begins audio capture in a background goroutine.
// It returns immediately. The capture runs until ctx is cancelled.
func (c *Capturer) Start(ctx context.Context) {
	ctx, c.cancelFunc = context.WithCancel(ctx)

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
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coINIT_MULTITHREADED)
	if hr != 0 && hr != 1 {
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

		sessionCtx, sessionCancel := context.WithCancel(ctx)
		c.mu.Lock()
		c.sessionCancel = sessionCancel
		c.mu.Unlock()

		err = c.runCaptureSession(sessionCtx, audioClient)
		sessionCancel()

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
func (c *Capturer) runCaptureSession(ctx context.Context, audioClient uintptr) error {
	format := PCM16Stereo48kHz()
	flags := uint32(AUDCLNT_STREAMFLAGS_LOOPBACK | AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY)

	hr, _ := comCall(audioClient, 3, // Initialize
		uintptr(AUDCLNT_SHAREMODE_SHARED),
		uintptr(flags),
		0, // buffer duration
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
			c.readBuffers(captureClient)
		}
	}
}

func (c *Capturer) readBuffers(captureClient uintptr) {
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
		if hr != 0 || numFrames == 0 {
			break
		}

		// Each frame = 4 bytes (2 channels * 16-bit)
		byteCount := int(numFrames) * 4
		if data != 0 && byteCount > 0 {
			buf := unsafe.Slice((*byte)(unsafe.Pointer(data)), byteCount)
			c.writer.Write(buf)
		}

		comCall(captureClient, 4, uintptr(numFrames)) // ReleaseBuffer
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
