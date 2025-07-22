//go:build windows

package domain

import (
	"os/exec"
	"syscall"
)

const (
	// CREATE_NO_WINDOW creates a process without a console window
	// This is different from DETACHED_PROCESS - still has console access but no window
	CREATE_NO_WINDOW = 0x08000000
)

// setupProcessAttributesAlt - Alternative approach using CREATE_NO_WINDOW
func setupProcessAttributesAlt(cmd *exec.Cmd) {
	// Alternative approach: CREATE_NEW_PROCESS_GROUP + CREATE_NO_WINDOW
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW,
		// CREATE_NEW_PROCESS_GROUP: Enables our termination logic
		// CREATE_NO_WINDOW: Hides console window but preserves console access
	}

	// Note: This approach:
	// 1. Child processes in separate group (enables graceful termination)
	// 2. Child processes have no visible window (cleaner)
	// 3. Master retains console signal handling
	// 4. Different from DETACHED_PROCESS - may work better with cmd.exe
}

// setupProcessAttributesMinimal - Minimal approach with no special flags
func setupProcessAttributesMinimal(cmd *exec.Cmd) {
	// Minimal approach: no special process creation flags
	// Let Windows handle signal routing naturally
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// No special creation flags - use default process creation
	}

	// Note: This approach:
	// 1. Natural Windows process creation
	// 2. Master retains normal signal handling
	// 3. May require different termination strategy
	// 4. Most compatible with different shell environments
}
