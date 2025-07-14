//go:build windows

package domain

import (
	"syscall"
)

// sendGracefulSignal sends Ctrl-Break event to the process on Windows systems
func (pc *processControl) sendGracefulSignal() error {
	if pc.process == nil {
		return nil
	}

	// Send Ctrl-Break event to the process
	// This is the graceful termination signal on Windows
	return sendCtrlBreakToProcess(pc.process.Pid)
}

// sendCtrlBreakToProcess sends a Ctrl-Break event to the process with id pid
func sendCtrlBreakToProcess(pid int) error {
	d, e := syscall.LoadDLL("kernel32.dll")
	if e != nil {
		return e
	}
	p, e := d.FindProc("GenerateConsoleCtrlEvent")
	if e != nil {
		return e
	}
	r, _, e := p.Call(uintptr(syscall.CTRL_BREAK_EVENT), uintptr(pid))
	if r == 0 {
		return e
	}
	return nil
}
