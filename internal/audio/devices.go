package audio

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procCoCreateInstance = modole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = modole32.NewProc("CoTaskMemFree")
	procPropVariantClear = modole32.NewProc("PropVariantClear")
)

// legacyDefaultDeviceID is what configs saved before devices were selectable.
const legacyDefaultDeviceID = "Default Output Device"

const (
	clsctxAll         = 0x17
	eRender           = 0
	eConsole          = 0
	deviceStateActive = 0x1
	stgmRead          = 0
	vtLPWSTR          = 31
	rpcEChangedMode   = 0x80010106

	// Buffer for the endpoint loopback stream, in 100ns units. Generous so
	// the 10ms polling loop never lets it overflow.
	endpointBufferDuration = 2_000_000 // 200ms
	// Buffer for the silent keep-alive render stream.
	keepAliveBufferDuration = 5_000_000 // 500ms
)

var (
	// {BCDE0395-E52F-467C-8E3D-C4579291692E}
	clsidMMDeviceEnumerator = syscall.GUID{
		Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C,
		Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E},
	}
	// {A95664D2-9614-4F35-A746-DE8DB63617E6}
	iidIMMDeviceEnumerator = syscall.GUID{
		Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35,
		Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6},
	}
	// {F294ACFC-3146-4483-A7BF-ADDCA7C260E2}
	iidIAudioRenderClient = syscall.GUID{
		Data1: 0xF294ACFC, Data2: 0x3146, Data3: 0x4483,
		Data4: [8]byte{0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2},
	}
	// PKEY_Device_FriendlyName: {A45C254E-DF1C-4EFD-8020-67D146A850E0}, 14
	pkeyDeviceFriendlyName = propertyKey{
		fmtid: syscall.GUID{
			Data1: 0xA45C254E, Data2: 0xDF1C, Data3: 0x4EFD,
			Data4: [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0},
		},
		pid: 14,
	}
)

type propertyKey struct {
	fmtid syscall.GUID
	pid   uint32
}

// propVariant is a PROPVARIANT with its value union left raw (24 bytes on x64).
type propVariant struct {
	vt  uint16
	_   [6]byte
	val [2]uintptr
}

// Device is an active audio output (render) endpoint.
type Device struct {
	ID        string // endpoint ID; stable across reboots, what configs store
	Name      string // friendly name, e.g. "Speakers (Realtek(R) Audio)"
	IsDefault bool   // Windows' current default output device
}

// IsSystemAudio reports whether a configured audio device means "capture all
// system audio except VRChat" (process loopback) rather than one specific
// output device. Empty is the default; "Default Output Device" is what the
// GUI saved before real devices were listed.
func IsSystemAudio(device string) bool {
	return device == "" || strings.EqualFold(device, legacyDefaultDeviceID)
}

// IMMDeviceEnumerator vtable (after IUnknown 0-2):
//
//	3=EnumAudioEndpoints, 4=GetDefaultAudioEndpoint, 5=GetDevice
//
// IMMDeviceCollection: 3=GetCount, 4=Item
// IMMDevice: 3=Activate, 4=OpenPropertyStore, 5=GetId, 6=GetState
// IPropertyStore: 5=GetValue

// ListOutputDevices returns the active audio output devices. Safe to call
// from any goroutine.
func ListOutputDevices() ([]Device, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if uninit, err := comInit(); err != nil {
		return nil, err
	} else if uninit {
		defer procCoUninitialize.Call()
	}

	enum, err := newDeviceEnumerator()
	if err != nil {
		return nil, err
	}
	defer comCall(enum, 2) // Release

	defaultID := ""
	var def uintptr
	if hr, _ := comCall(enum, 4, eRender, eConsole, uintptr(unsafe.Pointer(&def))); hr == 0 {
		defaultID, _ = deviceID(def)
		comCall(def, 2)
	}

	var coll uintptr
	if hr, _ := comCall(enum, 3, eRender, deviceStateActive, uintptr(unsafe.Pointer(&coll))); hr != 0 {
		return nil, fmt.Errorf("EnumAudioEndpoints failed: 0x%x", hr)
	}
	defer comCall(coll, 2)

	var count uint32
	if hr, _ := comCall(coll, 3, uintptr(unsafe.Pointer(&count))); hr != 0 {
		return nil, fmt.Errorf("IMMDeviceCollection.GetCount failed: 0x%x", hr)
	}

	var devices []Device
	for i := uint32(0); i < count; i++ {
		var dev uintptr
		if hr, _ := comCall(coll, 4, uintptr(i), uintptr(unsafe.Pointer(&dev))); hr != 0 {
			continue
		}
		id, err := deviceID(dev)
		if err == nil {
			name := friendlyName(dev)
			if name == "" {
				name = id
			}
			devices = append(devices, Device{ID: id, Name: name, IsDefault: id == defaultID})
		}
		comCall(dev, 2)
	}
	return devices, nil
}

// comInit initializes COM (MTA) on the current, locked thread. uninit reports
// whether the caller must call CoUninitialize when done.
func comInit() (uninit bool, err error) {
	hr, _, _ := procCoInitializeEx.Call(0, coINIT_MULTITHREADED)
	switch hr {
	case 0, 1: // S_OK, S_FALSE
		return true, nil
	case rpcEChangedMode: // already initialized as STA; usable as-is
		return false, nil
	}
	return false, fmt.Errorf("CoInitializeEx failed: 0x%x", hr)
}

func newDeviceEnumerator() (uintptr, error) {
	var enum uintptr
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0,
		clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enum)),
	)
	if hr != 0 {
		return 0, fmt.Errorf("creating MMDeviceEnumerator failed: 0x%x", hr)
	}
	return enum, nil
}

func deviceID(dev uintptr) (string, error) {
	var p uintptr
	if hr, _ := comCall(dev, 5, uintptr(unsafe.Pointer(&p))); hr != 0 {
		return "", fmt.Errorf("IMMDevice.GetId failed: 0x%x", hr)
	}
	defer procCoTaskMemFree.Call(p)
	return utf16PtrToString(p), nil
}

func friendlyName(dev uintptr) string {
	var store uintptr
	if hr, _ := comCall(dev, 4, stgmRead, uintptr(unsafe.Pointer(&store))); hr != 0 {
		return ""
	}
	defer comCall(store, 2)

	var pv propVariant
	if hr, _ := comCall(store, 5, uintptr(unsafe.Pointer(&pkeyDeviceFriendlyName)), uintptr(unsafe.Pointer(&pv))); hr != 0 {
		return ""
	}
	defer procPropVariantClear.Call(uintptr(unsafe.Pointer(&pv)))
	if pv.vt != vtLPWSTR {
		return ""
	}
	return utf16PtrToString(pv.val[0])
}

func utf16PtrToString(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Pointer(p + uintptr(n)*2)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), n))
}

// findOutputDevice returns the active output device matching want, by
// endpoint ID or (case-insensitively) by friendly name, so the CLI's
// --audio-device accepts either. The caller must Release the device.
func findOutputDevice(enum uintptr, want string) (uintptr, error) {
	idPtr, err := syscall.UTF16PtrFromString(want)
	if err == nil {
		var dev uintptr
		if hr, _ := comCall(enum, 5, uintptr(unsafe.Pointer(idPtr)), uintptr(unsafe.Pointer(&dev))); hr == 0 {
			var state uint32
			if hr, _ := comCall(dev, 6, uintptr(unsafe.Pointer(&state))); hr == 0 && state == deviceStateActive {
				return dev, nil
			}
			comCall(dev, 2)
		}
	}

	var coll uintptr
	if hr, _ := comCall(enum, 3, eRender, deviceStateActive, uintptr(unsafe.Pointer(&coll))); hr != 0 {
		return 0, fmt.Errorf("EnumAudioEndpoints failed: 0x%x", hr)
	}
	defer comCall(coll, 2)
	var count uint32
	comCall(coll, 3, uintptr(unsafe.Pointer(&count)))
	for i := uint32(0); i < count; i++ {
		var dev uintptr
		if hr, _ := comCall(coll, 4, uintptr(i), uintptr(unsafe.Pointer(&dev))); hr != 0 {
			continue
		}
		if strings.EqualFold(friendlyName(dev), want) {
			return dev, nil
		}
		comCall(dev, 2)
	}
	return 0, fmt.Errorf("output device %q is not connected", want)
}

// activateEndpointLoopback opens a loopback capture client on one specific
// output device, plus a silent render stream on the same device. Endpoint
// loopback only produces packets while the device is actually playing
// something, and FFmpeg stalls the entire stream while its audio pipe is
// starved, so the silent stream keeps the device (and the capture) running
// through silence — the same approach OBS uses for output capture.
//
// Returns the capture IAudioClient (caller Releases), the device's friendly
// name, and a stop function for the keep-alive stream.
func activateEndpointLoopback(want string) (client uintptr, name string, stopKeepAlive func(), err error) {
	enum, err := newDeviceEnumerator()
	if err != nil {
		return 0, "", nil, err
	}
	defer comCall(enum, 2)

	dev, err := findOutputDevice(enum, want)
	if err != nil {
		return 0, "", nil, err
	}
	defer comCall(dev, 2)

	stopKeepAlive, err = startSilentRender(dev)
	if err != nil {
		return 0, "", nil, fmt.Errorf("starting keep-alive stream: %w", err)
	}

	if hr, _ := comCall(dev, 3, // Activate
		uintptr(unsafe.Pointer(&IID_IAudioClient)),
		clsctxAll,
		0,
		uintptr(unsafe.Pointer(&client)),
	); hr != 0 {
		stopKeepAlive()
		return 0, "", nil, fmt.Errorf("activating loopback client failed: 0x%x", hr)
	}

	name = friendlyName(dev)
	if name == "" {
		name = want
	}
	return client, name, stopKeepAlive, nil
}

// startSilentRender plays a buffer of silence on dev and leaves the stream
// running, so the device keeps producing loopback packets during silence.
func startSilentRender(dev uintptr) (stop func(), err error) {
	var client uintptr
	if hr, _ := comCall(dev, 3, // Activate
		uintptr(unsafe.Pointer(&IID_IAudioClient)),
		clsctxAll,
		0,
		uintptr(unsafe.Pointer(&client)),
	); hr != 0 {
		return nil, fmt.Errorf("activating render client failed: 0x%x", hr)
	}
	release := func() { comCall(client, 2) }

	format := PCM16Stereo48kHz()
	if hr, _ := comCall(client, 3, // Initialize
		uintptr(AUDCLNT_SHAREMODE_SHARED),
		uintptr(AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM|AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY),
		keepAliveBufferDuration,
		0,
		uintptr(unsafe.Pointer(&format)),
		0,
	); hr != 0 {
		release()
		return nil, fmt.Errorf("render IAudioClient.Initialize failed: 0x%x", hr)
	}

	var frames uint32
	if hr, _ := comCall(client, 4, uintptr(unsafe.Pointer(&frames))); hr != 0 { // GetBufferSize
		release()
		return nil, fmt.Errorf("render GetBufferSize failed: 0x%x", hr)
	}

	var render uintptr
	if hr, _ := comCall(client, 14, // GetService
		uintptr(unsafe.Pointer(&iidIAudioRenderClient)),
		uintptr(unsafe.Pointer(&render)),
	); hr != 0 {
		release()
		return nil, fmt.Errorf("render GetService failed: 0x%x", hr)
	}

	// IAudioRenderClient: 3=GetBuffer, 4=ReleaseBuffer
	var data uintptr
	if hr, _ := comCall(render, 3, uintptr(frames), uintptr(unsafe.Pointer(&data))); hr == 0 {
		comCall(render, 4, uintptr(frames), AUDCLNT_BUFFERFLAGS_SILENT)
	}

	if hr, _ := comCall(client, 10); hr != 0 { // Start
		comCall(render, 2)
		release()
		return nil, fmt.Errorf("render Start failed: 0x%x", hr)
	}

	return func() {
		comCall(client, 11) // Stop
		comCall(render, 2)
		release()
	}, nil
}
