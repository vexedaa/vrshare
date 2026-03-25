//go:build windows

package main

import (
	"log"
	"os"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/vexedaa/vrshare/frontend"
	"github.com/vexedaa/vrshare/internal/gui"
)

const ATTACH_PARENT_PROCESS = ^uintptr(0) // (DWORD)-1

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole  = kernel32.NewProc("AttachConsole")
	procGetStdHandle   = kernel32.NewProc("GetStdHandle")
	procSetStdHandle   = kernel32.NewProc("SetStdHandle")
)

// hasConsole tries to attach to the parent process's console.
// Returns true if launched from a terminal (cmd, powershell, etc.).
// Returns false if double-clicked from Explorer (no parent console).
// Must be built with -ldflags "-H windowsgui" for this to work correctly.
func hasConsole() bool {
	r, _, _ := procAttachConsole.Call(uintptr(ATTACH_PARENT_PROCESS))
	if r == 0 {
		return false
	}
	// Reopen stdout/stderr to the attached console
	reattachStdio()
	return true
}

// reattachStdio reconnects os.Stdout and os.Stderr to the console
// after AttachConsole, since GUI subsystem apps don't have them by default.
func reattachStdio() {
	const STD_OUTPUT_HANDLE = uintptr(^uint32(11 - 1)) // -11
	const STD_ERROR_HANDLE = uintptr(^uint32(12 - 1))  // -12

	stdout, _, _ := procGetStdHandle.Call(STD_OUTPUT_HANDLE)
	stderr, _, _ := procGetStdHandle.Call(STD_ERROR_HANDLE)

	if stdout != 0 && stdout != ^uintptr(0) {
		os.Stdout = os.NewFile(stdout, "stdout")
	}
	if stderr != 0 && stderr != ^uintptr(0) {
		os.Stderr = os.NewFile(stderr, "stderr")
	}

	// Redirect log output to the new stderr
	log.SetOutput(os.Stderr)

	// Also set the CRT file descriptors via SetStdHandle
	procSetStdHandle.Call(STD_OUTPUT_HANDLE, stdout)
	procSetStdHandle.Call(STD_ERROR_HANDLE, stderr)

	_ = unsafe.Pointer(nil) // keep unsafe import
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
	})
	if err != nil {
		log.Fatal(err)
	}
}
