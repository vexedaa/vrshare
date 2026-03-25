package audio

import (
	"sync"
	"syscall"
	"unsafe"
)

// DLL and proc references
var (
	modole32                        = syscall.NewLazyDLL("ole32.dll")
	modcombase                      = syscall.NewLazyDLL("combase.dll")
	modmmdevapi                     = syscall.NewLazyDLL("MMDevAPI.dll")
	procCoInitializeEx              = modole32.NewProc("CoInitializeEx")
	procCoUninitialize              = modole32.NewProc("CoUninitialize")
	procRoInitialize                = modcombase.NewProc("RoInitialize")
	procRoUninitialize              = modcombase.NewProc("RoUninitialize")
	procActivateAudioInterfaceAsync = modmmdevapi.NewProc("ActivateAudioInterfaceAsync")
)

// COM threading model
const coINIT_MULTITHREADED = 0x0

// WASAPI constants
const (
	AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK      = 1
	PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE = 0
	PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE = 1
	AUDCLNT_SHAREMODE_SHARED                          = 0
	AUDCLNT_STREAMFLAGS_LOOPBACK                      = 0x00020000
	AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM                = 0x80000000
	AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY           = 0x08000000
	WAVE_FORMAT_PCM                                   = 1
	VT_BLOB                                           = 65
)

// VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK is the device ID for process loopback.
// From audioclientactivationparams.h: L"VAD\\Process_Loopback"
var virtualAudioDeviceProcessLoopback = syscall.StringToUTF16Ptr("VAD\\Process_Loopback")

// GUIDs — defined as raw bytes to avoid dependency on x/sys/windows
var (
	// {1CB9AD4C-DBFA-4c32-B178-C2F568A703B2}
	IID_IAudioClient = syscall.GUID{
		Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32,
		Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2},
	}
	// {C8ADBD64-E71E-48a0-A4DE-185C395CD317}
	IID_IAudioCaptureClient = syscall.GUID{
		Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0,
		Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17},
	}
	// {00000000-0000-0000-C000-000000000046}
	IID_IUnknown = syscall.GUID{
		Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	// {41D949AB-9862-444A-80F6-C261334DA5EB}
	IID_IActivateAudioInterfaceCompletionHandler = syscall.GUID{
		Data1: 0x41D949AB, Data2: 0x9862, Data3: 0x444A,
		Data4: [8]byte{0x80, 0xF6, 0xC2, 0x61, 0x33, 0x4D, 0xA5, 0xEB},
	}
	// {94EA2B94-E9CC-49E0-C0FF-EE64CA8F5B90}
	IID_IAgileObject = syscall.GUID{
		Data1: 0x94EA2B94, Data2: 0xE9CC, Data3: 0x49E0,
		Data4: [8]byte{0xC0, 0xFF, 0xEE, 0x64, 0xCA, 0x8F, 0x5B, 0x90},
	}
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
		BlockAlign:     4,      // 2 channels * 2 bytes
		AvgBytesPerSec: 192000, // 48000 * 4
		Size:           0,
	}
}

// AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS specifies the process to include/exclude.
type AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS struct {
	TargetProcessId     uint32
	ProcessLoopbackMode uint32
}

// AUDIOCLIENT_ACTIVATION_PARAMS wraps the activation type and params.
type AUDIOCLIENT_ACTIVATION_PARAMS struct {
	ActivationType        uint32
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
	_pad [4]byte // padding on 64-bit
	Data uintptr
}

// completionHandler implements IActivateAudioInterfaceCompletionHandler.
// It signals a channel when the async activation completes.
type completionHandler struct {
	vtbl     *completionHandlerVtbl
	refCount int32
	done     chan struct{}
	mu       sync.Mutex
}

type completionHandlerVtbl struct {
	QueryInterface    uintptr
	AddRef            uintptr
	Release           uintptr
	ActivateCompleted uintptr
}

var (
	handlerQueryInterfaceCallback    uintptr
	handlerAddRefCallback            uintptr
	handlerReleaseCallback           uintptr
	handlerActivateCompletedCallback uintptr
	callbacksOnce                    sync.Once
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
	if *iid == IID_IUnknown || *iid == IID_IActivateAudioInterfaceCompletionHandler || *iid == IID_IAgileObject {
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
