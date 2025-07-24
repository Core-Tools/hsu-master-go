//go:build !windows

package domain

import (
	"os"
	"syscall"
	"time"
)

// sendTerminationSignal sends SIGTERM to the process group on Unix systems
func sendTerminationSignal(pid int, idDead bool, timeout time.Duration) error {
	// Send SIGTERM to the process group (negative PID)
	// This ensures we terminate the entire process tree
	return syscall.Kill(-pid, syscall.SIGTERM)
}

func isProcessRunning(pid int) bool {
	// On Unix systems, FindProcess always succeeds and returns a Process
	// for the given pid, regardless of whether the process exists. To test whether
	// the process actually exists, see whether p.Signal(syscall.Signal(0)) reports
	// an error.
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	return process.Signal(syscall.Signal(0)) == nil
}
