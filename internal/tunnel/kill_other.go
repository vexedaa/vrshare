//go:build !windows

package tunnel

import "os/exec"

// killTree terminates the tunnel process. On non-Windows platforms the tunnel
// providers aren't launched through a shim, so killing the process directly is
// sufficient.
func killTree(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}
