//go:build windows

package domain

import (
	"fmt"
	"sync"
	"syscall"
	"time"
)

// Enhanced sendTerminationSignal using the improved console signal fix
func sendTerminationSignal(pid int, idDead bool, timeout time.Duration) error {
	if idDead {
		// Apply the AttachConsole dead PID hack to fix Windows signal handling
		// This restores Ctrl+C functionality for the master process
		return consoleSignalFix(pid)
	} else {
		// Send actual Ctrl+Break signal to alive process with safety checks
		return sendCtrlBreakToProcessSafe(pid, timeout)
	}
}

// Windows console operation lock to prevent race conditions
var consoleOperationLock sync.Mutex

// Windows process status constants
const (
	STILL_ACTIVE                      = 259
	WAIT_TIMEOUT                      = 0x00000102
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
)

// isProcessRunning checks if a Windows process is still running
func isProcessRunning(pid int) bool {
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

// consoleSignalFix implements the AttachConsole dead PID hack to fix Windows signal handling
func consoleSignalFix(deadPID int) error {
	consoleOperationLock.Lock()
	defer consoleOperationLock.Unlock()

	// Verify the PID is actually dead before using it for console reset
	if isProcessRunning(deadPID) {
		return fmt.Errorf("cannot use alive process PID %d for console signal fix", deadPID)
	}

	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return fmt.Errorf("failed to load kernel32.dll: %v", err)
	}
	defer dll.Release()

	// Try to attach to the dead process - this triggers console state reset
	err = attachConsole(dll, deadPID)
	if err != nil {
		// Expected to fail for dead process - that's the magic!
		return nil
	}
	// Unexpected success - should not happen with dead PID
	return fmt.Errorf("⚠️  Warning: AttachConsole unexpectedly succeeded for PID %d\n", deadPID)
}

// sendCtrlBreakToProcessSafe sends Ctrl+Break with liveness check and timeout protection
func sendCtrlBreakToProcessSafe(pid int, timeout time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID: %d", pid)
	}

	consoleOperationLock.Lock()
	defer consoleOperationLock.Unlock()

	// Check if process is alive before sending signal
	if !isProcessRunning(pid) {
		return fmt.Errorf("process PID %d is not alive, skipping signal", pid)
	}

	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return fmt.Errorf("failed to load kernel32.dll: %v", err)
	}
	defer dll.Release()

	// Use timeout to prevent hanging
	done := make(chan error, 1)
	go func() {
		done <- generateConsoleCtrlEvent(dll, pid)
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("failed to send Ctrl+Break to PID %d: %v", pid, err)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout sending Ctrl+Break to PID %d after %v", pid, timeout)
	}
}

// Helper functions for Windows API calls
func attachConsole(dll *syscall.DLL, pid int) error {
	attachConsole, err := dll.FindProc("AttachConsole")
	if err != nil {
		return err
	}

	result, _, err := attachConsole.Call(uintptr(pid))
	if result == 0 {
		// This is expected to fail for dead PIDs - that's the magic!
		return err
	}
	return nil
}

func generateConsoleCtrlEvent(dll *syscall.DLL, pid int) error {
	generateConsoleCtrlEvent, err := dll.FindProc("GenerateConsoleCtrlEvent")
	if err != nil {
		return err
	}

	result, _, err := generateConsoleCtrlEvent.Call(
		uintptr(syscall.CTRL_BREAK_EVENT),
		uintptr(pid),
	)
	if result == 0 {
		return err
	}
	return nil
}

// Advanced console reset strategy (alternative approach)
func resetConsoleSession() error {
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}
	defer dll.Release()

	// Free current console
	if err := freeConsole(dll); err != nil {
		// FreeConsole failed (might be expected)
	}

	// Allocate new console
	if err := allocConsole(dll); err != nil {
		return fmt.Errorf("failed to allocate new console: %v", err)
	}

	return nil
}

// Console management helpers for advanced scenarios
func freeConsole(dll *syscall.DLL) error {
	freeConsole, err := dll.FindProc("FreeConsole")
	if err != nil {
		return err
	}

	result, _, err := freeConsole.Call()
	if result == 0 {
		return err
	}
	return nil
}

func allocConsole(dll *syscall.DLL) error {
	allocConsole, err := dll.FindProc("AllocConsole")
	if err != nil {
		return err
	}

	result, _, err := allocConsole.Call()
	if result == 0 {
		return err
	}
	return nil
}
