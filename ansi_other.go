//go:build !windows

package main

// enableWindowsANSI is a no-op on non-Windows platforms.
func enableWindowsANSI() {}
