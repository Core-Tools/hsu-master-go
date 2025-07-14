//go:build windows

package domain

import (
	"fmt"
	"os"
	"syscall"
)

// verifyProcessRunning checks if a process is actually running on Windows systems
func verifyProcessRunning(process *os.Process) error {
	if process == nil {
		return fmt.Errorf("process is nil")
	}

	// On Windows, we need to use WinAPI to check if process is running
	// Get process handle
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(process.Pid))
	if err != nil {
		return fmt.Errorf("process is not running or not accessible: %v", err)
	}
	defer syscall.CloseHandle(handle)

	// Get exit code
	var exitCode uint32
	err = syscall.GetExitCodeProcess(handle, &exitCode)
	if err != nil {
		return fmt.Errorf("failed to get process exit code: %v", err)
	}

	// STILL_ACTIVE = 259 means process is still running
	const STILL_ACTIVE = 259
	if exitCode != STILL_ACTIVE {
		return fmt.Errorf("process has exited with code %d", exitCode)
	}

	return nil
}
