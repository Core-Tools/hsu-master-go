//go:build windows

package domain

import (
	"fmt"
	"syscall"
)

func prepareOSConsole() error {
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}
	if isConsoleAllocated(dll) {
		fmt.Println("----------- Console already allocated")
		return nil
	}

	fmt.Println("----------- Freeing console")
	if err := callFreeConsole(dll); err != nil {
		return fmt.Errorf("prepare OS console: %v", err)
	}
	fmt.Println("----------- Console freed")

	fmt.Println("----------- Allocating console")
	if err := callAllocConsole(dll); err != nil {
		return fmt.Errorf("prepare OS console: %v", err)
	}
	fmt.Println("----------- Console allocated")
	return nil
}

func isConsoleAllocated(dll *syscall.DLL) bool {
	getConsoleWindow, err := dll.FindProc("GetConsoleWindow")
	if err != nil {
		return false
	}
	result, _, _ := getConsoleWindow.Call()
	return result != 0
}

func callAllocConsole(dll *syscall.DLL) error {
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
