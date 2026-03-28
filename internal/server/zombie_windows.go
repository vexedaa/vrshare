//go:build windows

package server

import (
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modkernel32z          = syscall.NewLazyDLL("kernel32.dll")
	procSnapshotProcs     = modkernel32z.NewProc("CreateToolhelp32Snapshot")
	procProcFirst         = modkernel32z.NewProc("Process32FirstW")
	procProcNext          = modkernel32z.NewProc("Process32NextW")
	procOpenProcess       = modkernel32z.NewProc("OpenProcess")
	procTerminateProcess  = modkernel32z.NewProc("TerminateProcess")

	modiphlpapi           = syscall.NewLazyDLL("iphlpapi.dll")
	procGetExtendedTcpTable = modiphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	thSnapProcess        = 0x00000002
	processTerminate     = 0x0001
	tcpTableOwnerPidAll  = 5
	mibTCPStateListen    = 2
)

type procEntry struct {
	Size      uint32
	Usage     uint32
	PID       uint32
	HeapID    uintptr
	ModuleID  uint32
	Threads   uint32
	ParentPID uint32
	PriClass  int32
	Flags     uint32
	ExeFile   [260]uint16
}

type mibTCPRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

// zombieNames are child process names that VRShare spawns.
// If one of these is orphaned (parent dead), it's safe to kill.
var zombieNames = map[string]bool{
	"ffmpeg.exe":      true,
	"cloudflared.exe": true,
}

// killZombies kills orphaned child processes from previous VRShare
// sessions that may have been left behind after a crash or force-kill.
// It also kills any process holding our configured port.
func killZombies(port int) {
	procs := listProcesses()
	if len(procs) == 0 {
		return
	}

	livingPIDs := make(map[uint32]bool, len(procs))
	for _, p := range procs {
		livingPIDs[p.PID] = true
	}

	myPID := uint32(os.Getpid())
	var killed int

	// Kill orphaned child processes (parent no longer exists)
	for _, p := range procs {
		if p.PID == myPID || p.ParentPID == myPID {
			continue
		}
		name := strings.ToLower(syscall.UTF16ToString(p.ExeFile[:]))
		if !zombieNames[name] {
			continue
		}
		if livingPIDs[p.ParentPID] {
			continue // parent still alive, not an orphan
		}
		if terminateByPID(p.PID) {
			log.Printf("Killed orphaned %s (PID %d)", name, p.PID)
			killed++
		}
	}

	// Kill whatever is holding our port (if it's not us)
	if holderPID := findPortHolder(port); holderPID != 0 && holderPID != myPID {
		// Identify the holder before killing
		holderName := "unknown"
		for _, p := range procs {
			if p.PID == holderPID {
				holderName = syscall.UTF16ToString(p.ExeFile[:])
				break
			}
		}
		if terminateByPID(holderPID) {
			log.Printf("Killed zombie %s (PID %d) holding port %d", holderName, holderPID, port)
			killed++
		}
	}

	if killed > 0 {
		log.Printf("Cleaned up %d zombie process(es)", killed)
	}
}

func listProcesses() []procEntry {
	snap, _, _ := procSnapshotProcs.Call(thSnapProcess, 0)
	if snap == ^uintptr(0) {
		return nil
	}
	defer syscall.CloseHandle(syscall.Handle(snap))

	var entry procEntry
	entry.Size = uint32(unsafe.Sizeof(entry))

	var out []procEntry
	ok, _, _ := procProcFirst.Call(snap, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		out = append(out, entry)
		entry.Size = uint32(unsafe.Sizeof(entry))
		ok, _, _ = procProcNext.Call(snap, uintptr(unsafe.Pointer(&entry)))
	}
	return out
}

func terminateByPID(pid uint32) bool {
	h, _, _ := procOpenProcess.Call(processTerminate, 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	ret, _, _ := procTerminateProcess.Call(h, 1)
	return ret != 0
}

// findPortHolder returns the PID of the process listening on the given TCP port,
// or 0 if no process is found.
func findPortHolder(port int) uint32 {
	// Convert port to network byte order (big-endian) as used by the API
	netPort := uint32((port>>8)&0xFF) | uint32((port&0xFF)<<8)

	// First call to get required buffer size
	var size uint32
	procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 1,
		syscall.AF_INET, tcpTableOwnerPidAll, 0)
	if size == 0 {
		return 0
	}

	buf := make([]byte, size)
	ret, _, _ := procGetExtendedTcpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		1, // order
		syscall.AF_INET,
		tcpTableOwnerPidAll,
		0,
	)
	if ret != 0 {
		return 0
	}

	// Parse the table: first DWORD is count, followed by rows
	if len(buf) < 4 {
		return 0
	}
	count := *(*uint32)(unsafe.Pointer(&buf[0]))
	rowSize := uint32(unsafe.Sizeof(mibTCPRowOwnerPID{}))

	for i := uint32(0); i < count; i++ {
		offset := 4 + i*rowSize
		if offset+rowSize > uint32(len(buf)) {
			break
		}
		row := (*mibTCPRowOwnerPID)(unsafe.Pointer(&buf[offset]))
		if row.State == mibTCPStateListen && row.LocalPort == netPort {
			return row.OwningPID
		}
	}

	return 0
}

// portHolderInfo returns a description of what's holding a port, for error messages.
func portHolderInfo(port int) string {
	pid := findPortHolder(port)
	if pid == 0 {
		return ""
	}
	procs := listProcesses()
	for _, p := range procs {
		if p.PID == pid {
			name := syscall.UTF16ToString(p.ExeFile[:])
			return fmt.Sprintf("%s (PID %d)", name, pid)
		}
	}
	return fmt.Sprintf("PID %d", pid)
}
