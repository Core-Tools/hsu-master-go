//go:build windows

package domain

import (
	"fmt"
	"sync"
	"syscall"
	"time"
)

// Enhanced sendGracefulSignal using the improved console signal fix
func (pc *processControl) sendGracefulSignal(attachConsole bool) error {
	if pc.process == nil {
		return nil
	}

	pid := pc.process.Pid

	if attachConsole {
		// Apply the AttachConsole dead PID hack to fix Windows signal handling
		// This restores Ctrl+C functionality for the master process
		return consoleSignalFix(pid)
	} else {
		// Send actual Ctrl+Break signal to alive process with safety checks
		timeout := 10 * time.Second // Prevent hanging
		return sendCtrlBreakToProcessSafe(pid, timeout)
	}
}

// Legacy function maintained for compatibility
var legacyConsoleOperationLock sync.Mutex

// sendCtrlBreakToProcess - Legacy function with basic functionality
func sendCtrlBreakToProcess(pid int, attachConsole bool) error {
	legacyConsoleOperationLock.Lock()
	defer legacyConsoleOperationLock.Unlock()

	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}
	defer dll.Release()

	if attachConsole {
		fmt.Printf("🔧 Applying legacy console signal fix using PID %d\n", pid)
		err = callAttachConsole(dll, pid)
		if err != nil {
			fmt.Printf("📡 AttachConsole failed as expected (PID %d): %v\n", pid, err)
		}
		return nil // Early return after console fix
	}

	fmt.Printf("📡 Sending Ctrl+Break to process PID %d (legacy method)\n", pid)
	return callGenerateConsoleCtrlEvent(dll, pid)
}

// Legacy helper functions are defined in process_control_windows_improved.go

// Note: callAttachConsole and callGenerateConsoleCtrlEvent are defined in process_control_windows_improved.go
