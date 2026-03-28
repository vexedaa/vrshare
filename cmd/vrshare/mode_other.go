//go:build !windows

package main

import "log"

func hasConsole() bool {
	return true
}

func attachParentConsole() {}

func launchGUI() {
	log.Fatal("GUI mode is only supported on Windows")
}
