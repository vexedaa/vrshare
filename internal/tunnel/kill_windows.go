//go:build windows

package tunnel

import (
	"os/exec"
	"strconv"
	"syscall"
)

// killTree terminates the process and all of its descendants. cloudflared is
// frequently launched through a launcher/shim (e.g. the Chocolatey
// chocolatey\bin\cloudflared.exe shim) that spawns the real binary as a child;
// killing only the direct process orphans that child and leaks a live tunnel.
// taskkill /T walks and kills the whole tree.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	kill.Run()
	// Belt-and-suspenders: also signal the direct process in case taskkill is
	// unavailable. Harmless if the tree kill already reaped it.
	cmd.Process.Kill()
}
