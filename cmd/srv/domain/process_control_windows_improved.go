//go:build windows

package domain

import (
	"fmt"
	"sync"
	"syscall"
	"time"
)

// Windows console operation lock to prevent race conditions
var consoleOperationLock sync.Mutex

// Windows process status constants
const (
	STILL_ACTIVE                      = 259
	WAIT_TIMEOUT                      = 0x00000102
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
)

// isProcessAlive checks if a Windows process is still running
func isProcessAlive(pid int) bool {
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
	if isProcessAlive(deadPID) {
		return fmt.Errorf("cannot use alive process PID %d for console signal fix", deadPID)
	}

	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return fmt.Errorf("failed to load kernel32.dll: %v", err)
	}
	defer dll.Release()

	fmt.Printf("🔧 Applying console signal fix using dead PID %d\n", deadPID)

	// Try to attach to the dead process - this triggers console state reset
	err = callAttachConsole(dll, deadPID)
	if err != nil {
		// Expected to fail for dead process - that's the magic!
		fmt.Printf("📡 AttachConsole failed as expected (PID %d dead): %v\n", deadPID, err)
	} else {
		// Unexpected success - should not happen with dead PID
		fmt.Printf("⚠️  Warning: AttachConsole unexpectedly succeeded for PID %d\n", deadPID)
	}

	fmt.Printf("✅ Console signal fix applied, master Ctrl+C should work now\n")
	return nil
}

// sendCtrlBreakToProcessSafe sends Ctrl+Break with liveness check and timeout protection
func sendCtrlBreakToProcessSafe(pid int, timeout time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID: %d", pid)
	}

	// Check if process is alive before sending signal
	if !isProcessAlive(pid) {
		return fmt.Errorf("process PID %d is not alive, skipping signal", pid)
	}

	consoleOperationLock.Lock()
	defer consoleOperationLock.Unlock()

	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return fmt.Errorf("failed to load kernel32.dll: %v", err)
	}
	defer dll.Release()

	fmt.Printf("📡 Sending Ctrl+Break to alive process PID %d\n", pid)

	// Use timeout to prevent hanging
	done := make(chan error, 1)
	go func() {
		done <- callGenerateConsoleCtrlEvent(dll, pid)
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("failed to send Ctrl+Break to PID %d: %v", pid, err)
		}
		fmt.Printf("✅ Ctrl+Break sent successfully to PID %d\n", pid)
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout sending Ctrl+Break to PID %d after %v", pid, timeout)
	}
}

// Note: sendGracefulSignal is implemented in process_control_windows.go

// Batch termination with liveness checks for multiple processes
func terminateAliveProcessesSafe(pids []int, timeout time.Duration) []error {
	var errors []error

	for _, pid := range pids {
		if pid <= 0 {
			continue
		}

		// Only send signals to alive processes
		if isProcessAlive(pid) {
			if err := sendCtrlBreakToProcessSafe(pid, timeout); err != nil {
				errors = append(errors, fmt.Errorf("PID %d: %v", pid, err))
			}
		} else {
			fmt.Printf("⏭️  Skipping signal to dead process PID %d\n", pid)
		}
	}

	return errors
}

// Helper functions for Windows API calls
func callAttachConsole(dll *syscall.DLL, pid int) error {
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

func callGenerateConsoleCtrlEvent(dll *syscall.DLL, pid int) error {
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

// Console management helpers for advanced scenarios
func freeConsole() error {
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}
	defer dll.Release()

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

func allocConsole() error {
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}
	defer dll.Release()

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

// Advanced console reset strategy (alternative approach)
func resetConsoleSession() error {
	fmt.Println("🔄 Attempting advanced console session reset...")

	// Free current console
	if err := freeConsole(); err != nil {
		fmt.Printf("⚠️  FreeConsole failed (might be expected): %v\n", err)
	}

	// Allocate new console
	if err := allocConsole(); err != nil {
		return fmt.Errorf("failed to allocate new console: %v", err)
	}

	fmt.Println("✅ Console session reset complete")
	return nil
}
