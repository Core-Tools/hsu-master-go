//go:build !windows

package domain

import (
	"fmt"
	"os"
	"syscall"
)

// verifyProcessRunning checks if a process is actually running on Unix systems
func verifyProcessRunning(process *os.Process) error {
	if process == nil {
		return fmt.Errorf("process is nil")
	}

	// Send signal 0 to check if process exists
	// Signal 0 doesn't actually send a signal, but checks if we can send one
	err := process.Signal(syscall.Signal(0))
	if err != nil {
		return fmt.Errorf("process is not running or not accessible: %v", err)
	}

	return nil
}
