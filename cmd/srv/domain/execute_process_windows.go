//go:build windows

package domain

import (
	"os/exec"
	"syscall"
)

// setupProcessAttributes configures Windows-specific process attributes
func setupProcessAttributes(cmd *exec.Cmd) {
	// On Windows, create a new process group so we can send Ctrl-Break events
	// to the entire process tree
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}

	// Note: Process termination is now handled by ProcessControl.terminateProcess()
	// which works uniformly for both spawned and attached processes
}
