//go:build windows

package domain

import (
	"fmt"
	"sync"
	"syscall"
)

// sendGracefulSignal sends Ctrl-Break event to the process on Windows systems
func (pc *processControl) sendGracefulSignal(attachConsole bool) error {
	if pc.process == nil {
		return nil
	}

	// Send Ctrl-Break event to the process
	// This is the graceful termination signal on Windows
	return sendCtrlBreakToProcess(pc.process.Pid, attachConsole)
}

var consoleOperationLock sync.Mutex

// sendCtrlBreakToProcess sends a Ctrl-Break event to the process with id pid
func sendCtrlBreakToProcess(pid int, attachConsole bool) error {
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}

	///*
	//consoleOperationLock.Lock()
	//defer consoleOperationLock.Unlock()

	/*
		fmt.Println("++++++++++++ Freeing console")

		err = callFreeConsole(dll)
		if err != nil {
			return err
		}
	*/

	if attachConsole {
		fmt.Println("++++++++++++ Attaching console")

		err = callAttachConsole(dll, pid)
		if err != nil {
			return err
		}

		return nil // We attach console only for restart,
		// the process is already not alived and
		// sendCtrlBreakToProcess will fail anyway
	}

	/*
		fmt.Println("++++++++++++ Resetting console signals")

		err = resetConsoleSignals(true)
		if err != nil {
			return err
		}
	*/

	fmt.Println("++++++++++++ Generating console ctrl event")

	return callGenerateConsoleCtrlEvent(dll, pid)
}

func callFreeConsole(dll *syscall.DLL) error {
	callFreeConsole, err := dll.FindProc("FreeConsole")
	if err != nil {
		return err
	}
	result, _, err := callFreeConsole.Call()
	if result == 0 {
		return err
	}
	return nil
}

func callAttachConsole(dll *syscall.DLL, pid int) error {
	attachConsole, err := dll.FindProc("AttachConsole")
	if err != nil {
		return err
	}
	result, _, err := attachConsole.Call(uintptr(pid))
	if result == 0 {
		return err
	}
	return nil
}

func callGenerateConsoleCtrlEvent(dll *syscall.DLL, pid int) error {
	generateConsoleCtrlEvent, err := dll.FindProc("GenerateConsoleCtrlEvent")
	if err != nil {
		return err
	}
	result, _, err := generateConsoleCtrlEvent.Call(uintptr(syscall.CTRL_BREAK_EVENT), uintptr(pid))
	if result == 0 {
		return err
	}
	return nil
}
