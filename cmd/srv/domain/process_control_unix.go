//go:build !windows

package domain

import (
	"syscall"
)

// sendGracefulSignal sends SIGTERM to the process group on Unix systems
func (pc *processControl) sendGracefulSignal(attachConsole bool) error {
	if pc.process == nil {
		return nil
	}

	// Send SIGTERM to the process group (negative PID)
	// This ensures we terminate the entire process tree
	return syscall.Kill(-pc.process.Pid, syscall.SIGTERM)
}
