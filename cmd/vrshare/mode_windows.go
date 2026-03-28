//go:build windows

package main

import (
	"log"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/vexedaa/vrshare/frontend"
	"github.com/vexedaa/vrshare/internal/gui"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole       = kernel32.NewProc("AttachConsole")
	procCreateToolhelp32    = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32First      = kernel32.NewProc("Process32FirstW")
	procProcess32Next       = kernel32.NewProc("Process32NextW")
	procGetCurrentProcessId = kernel32.NewProc("GetCurrentProcessId")
)

type processEntry32 struct {
	dwSize              uint32
	cntUsage            uint32
	th32ProcessID       uint32
	th32DefaultHeapID   uintptr
	th32ModuleID        uint32
	cntThreads          uint32
	th32ParentProcessID uint32
	pcPriClassBase      int32
	dwFlags             uint32
	szExeFile           [260]uint16
}

// hasConsole returns true if launched from a terminal (cmd, powershell, bash, etc.)
// Returns false if launched from Explorer or other GUI shells (double-click).
func hasConsole() bool {
	parentName := strings.ToLower(getParentProcessName())
	terminals := []string{"cmd.exe", "powershell.exe", "pwsh.exe", "bash.exe",
		"wsl.exe", "windowsterminal.exe", "conhost.exe", "mintty.exe",
		"alacritty.exe", "wezterm-gui.exe", "hyper.exe"}
	for _, t := range terminals {
		if parentName == t {
			return true
		}
	}
	return false
}

func getParentProcessName() string {
	const TH32CS_SNAPPROCESS = 0x00000002

	pid, _, _ := procGetCurrentProcessId.Call()

	snap, _, _ := procCreateToolhelp32.Call(TH32CS_SNAPPROCESS, 0)
	if snap == ^uintptr(0) {
		return ""
	}
	defer syscall.CloseHandle(syscall.Handle(snap))

	var entry processEntry32
	entry.dwSize = uint32(unsafe.Sizeof(entry))

	// Find our process to get parent PID
	var parentPID uint32
	ok, _, _ := procProcess32First.Call(snap, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		if entry.th32ProcessID == uint32(pid) {
			parentPID = entry.th32ParentProcessID
			break
		}
		entry.dwSize = uint32(unsafe.Sizeof(entry))
		ok, _, _ = procProcess32Next.Call(snap, uintptr(unsafe.Pointer(&entry)))
	}
	if parentPID == 0 {
		return ""
	}

	// Find parent process name
	entry.dwSize = uint32(unsafe.Sizeof(entry))
	ok, _, _ = procProcess32First.Call(snap, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		if entry.th32ProcessID == parentPID {
			return syscall.UTF16ToString(entry.szExeFile[:])
		}
		entry.dwSize = uint32(unsafe.Sizeof(entry))
		ok, _, _ = procProcess32Next.Call(snap, uintptr(unsafe.Pointer(&entry)))
	}
	return ""
}

// attachParentConsole attaches to the parent process's console so that
// CLI output works when the binary is built with -H=windowsgui.
func attachParentConsole() {
	const ATTACH_PARENT_PROCESS = ^uint32(0) // (DWORD)-1
	r, _, _ := procAttachConsole.Call(uintptr(ATTACH_PARENT_PROCESS))
	if r == 0 {
		return
	}
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err == nil {
		os.Stdout = out
		os.Stderr = out
	}
}

func launchGUI() {
	app := gui.NewApp()

	err := wails.Run(&options.App{
		Title:     "VRShare",
		Width:     900,
		Height:    600,
		MinWidth:  700,
		MinHeight: 400,
		AssetServer: &assetserver.Options{
			Assets: frontend.Assets,
		},
		OnStartup:     app.Startup,
		OnShutdown:    app.Shutdown,
		OnBeforeClose: app.BeforeClose,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			Messages: &windows.Messages{
				InstallationRequired: "VRShare requires the WebView2 runtime.",
				Webview2NotInstalled: "WebView2 is not installed on your system.\nVRShare needs it to display its interface.\n\nClick OK to download and install it automatically.",
				MissingRequirements:  "VRShare is missing a required component.",
				Error:                "An error occurred while starting VRShare",
				FailedToInstall:      "WebView2 installation failed.\nPlease install it manually from:\nhttps://developer.microsoft.com/en-us/microsoft-edge/webview2/",
				DownloadPage:         "https://developer.microsoft.com/en-us/microsoft-edge/webview2/",
				PressOKToInstall:     "Press OK to install the WebView2 runtime, or Cancel to exit.",
				ContactAdmin:         "If the problem persists, please open an issue at https://github.com/vexedaa/vrshare/issues",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
