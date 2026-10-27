package main

import "golang.org/x/sys/windows"

var (
	procGetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("GetForegroundWindow")
	procGetConsoleWindow    = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")
)

// promptWindow is the window a Windows Hello prompt shows over: the one in
// front (the terminal the command was typed in; Windows Terminal hides its
// console window), else this console's.
func promptWindow() uint64 {
	if h, _, _ := procGetForegroundWindow.Call(); h != 0 {
		return uint64(h)
	}
	h, _, _ := procGetConsoleWindow.Call()
	return uint64(h)
}
