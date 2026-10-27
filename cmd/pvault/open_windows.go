package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// shellOpen opens target, a file pvault wrote or an https address, with its
// default application (ShellExecute), which needs no command line to quote.
func shellOpen(target string) {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return
	}
	windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// revealInExplorer selects p in an Explorer window. Explorer reads its own
// command line, /select, then the path in quotes (which a Windows path can't
// contain), so it is passed as written.
func revealInExplorer(p string) {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + p + `"`}
	cmd.Run()
}
