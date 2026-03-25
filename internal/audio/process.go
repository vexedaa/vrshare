package audio

import (
	"strings"
	"syscall"
	"unsafe"
)

var (
	modkernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procCreateToolhelp32Snapshot = modkernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = modkernel32.NewProc("Process32FirstW")
	procProcess32NextW           = modkernel32.NewProc("Process32NextW")
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
