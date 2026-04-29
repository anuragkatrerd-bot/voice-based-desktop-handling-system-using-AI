//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// enableWindowsANSI enables ANSI escape codes on Windows 10+.
// This gives coloured output identical to Linux/macOS.
func enableWindowsANSI() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	getStdHandle := kernel32.NewProc("GetStdHandle")

	const stdOutputHandle = ^uintptr(10) // -11
	handle, _, _ := getStdHandle.Call(stdOutputHandle)

	var mode uint32
	getConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode)))
	// ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004
	setConsoleMode.Call(handle, uintptr(mode|0x0004))
}
