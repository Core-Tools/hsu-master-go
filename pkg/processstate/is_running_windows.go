//go:build windows

package processstate

import (
	"syscall"
)

// Windows process status constants
const (
	STILL_ACTIVE                      = 259
	WAIT_TIMEOUT                      = 0x00000102
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
)

// isProcessRunning checks if a Windows process is still running
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Open process handle with minimal rights needed for status check
	handle, err := syscall.OpenProcess(
		PROCESS_QUERY_LIMITED_INFORMATION, // Minimal access rights
		false,                             // Don't inherit handle
		uint32(pid),
	)
	if err != nil {
		return false // Process doesn't exist or access denied
	}
	defer syscall.CloseHandle(handle)

	// Check process exit code
	var exitCode uint32
	err = syscall.GetExitCodeProcess(handle, &exitCode)
	if err != nil {
		return false // Can't get exit code, assume dead
	}

	// STILL_ACTIVE means process is running
	return exitCode == STILL_ACTIVE
}
