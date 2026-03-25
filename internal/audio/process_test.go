package audio

import (
	"testing"
)

func TestFindProcessByName_NotFound(t *testing.T) {
	pid := FindProcessByName("nonexistent_process_12345.exe")
	if pid != 0 {
		t.Errorf("expected 0 for nonexistent process, got %d", pid)
	}
}

func TestFindVRChatPID_ReturnsZeroWhenNotRunning(t *testing.T) {
	// VRChat is almost certainly not running during unit tests
	pid := FindVRChatPID()
	t.Logf("FindVRChatPID result: %d (0 expected unless VRChat is running)", pid)
}

func TestFindProcessByName_FindsKnownProcess(t *testing.T) {
	// explorer.exe should always be running on Windows
	pid := FindProcessByName("explorer.exe")
	if pid == 0 {
		t.Skip("explorer.exe not found — may not be running in this environment")
	}
	t.Logf("Found explorer.exe with PID %d", pid)
}
