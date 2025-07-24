//go:build !windows

package processstate

import (
	"os"
	"syscall"
)

func IsProcessRunning(pid int) bool {
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
