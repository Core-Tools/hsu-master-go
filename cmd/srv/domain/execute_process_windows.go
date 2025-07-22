//go:build windows

package domain

import (
	"fmt"
	"os/exec"
	"syscall"
)

// consoleCtrlHandler is the function that will be called when a console control event occurs.
func consoleCtrlHandler(controlType uint) uint {
	switch controlType {
	case syscall.CTRL_C_EVENT:
		fmt.Println("\n+++++++++++++++++ CTRL+C event received. Performing graceful shutdown...")
		return 1 // Indicate that the event was handled
	case syscall.CTRL_CLOSE_EVENT:
		fmt.Println("\n+++++++++++++++++ Console close event received. Performing graceful shutdown...")
		return 1 // Indicate that the event was handled
	case syscall.CTRL_BREAK_EVENT:
		fmt.Println("\n+++++++++++++++++ CTRL+BREAK event received.")
		return 1
	default:
		return 0 // Let the default handler process other events
	}
}

func resetConsoleSignals(setHandler bool) error {
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}

	setConsoleCtrlHandler, err := dll.FindProc("SetConsoleCtrlHandler")
	if err != nil {
		return err
	}

	handlerCallback := uintptr(0)
	setFlag := 0
	if setHandler {
		handlerCallback = syscall.NewCallback(consoleCtrlHandler)
		setFlag = 1
	}

	result, _, err := setConsoleCtrlHandler.Call(handlerCallback, uintptr(setFlag))
	if result == 0 {
		return err
	}

	return nil
}

// setupProcessAttributes configures Windows-specific process attributes
func setupProcessAttributes(cmd *exec.Cmd) {
	// Try simpler approach: only CREATE_NEW_PROCESS_GROUP
	// This isolates children for termination but preserves master's signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		// CREATE_NEW_PROCESS_GROUP: Enables our termination logic (sendCtrlBreak)
		// No DETACHED_PROCESS: Preserves master's console signal handling
	}

	// Note: This approach:
	// 1. Child processes in separate group (enables graceful termination)
	// 2. Master retains normal console signal handling in cmd.exe
	// 3. Our sendCtrlBreakToProcess() can terminate children gracefully
	// 4. Simpler signal routing - less interference during restart cycles
}
