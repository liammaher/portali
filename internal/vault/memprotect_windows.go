package vault

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procGetErrorMode = kernel32.NewProc("GetErrorMode")
	procWerGetFlags  = kernel32.NewProc("WerGetFlags")
	procWerSetFlags  = kernel32.NewProc("WerSetFlags")
)

// werFaultReportingFlagNoHeap asks Windows Error Reporting to leave heap
// memory, where the keys are, out of its crash reports
// (WER_FAULT_REPORTING_FLAG_NOHEAP).
const werFaultReportingFlagNoHeap = 1

// lockMemory keeps the byte slice's page(s) out of the page file.
// Best-effort: failure is silently ignored (the working set may be too small).
func lockMemory(b []byte) {
	if len(b) > 0 {
		_ = windows.VirtualLock(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	}
}

// unlockMemory unlocks previously locked pages; callers clear them first,
// since Windows moves a page it finds unlocked out of memory.
// Best-effort: failure is silently ignored.
func unlockMemory(b []byte) {
	if len(b) > 0 {
		_ = windows.VirtualUnlock(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	}
}

// disableCoreDumps is Windows' nearest to RLIMIT_CORE 0: no crash dialog, and
// no heap in the crash reports Windows Error Reporting collects. Both are
// added to what the process has, which the Go runtime set too (no fault UI),
// never put in its place. Best-effort: failure is silently ignored.
func disableCoreDumps() {
	if procGetErrorMode.Find() == nil {
		mode, _, _ := procGetErrorMode.Call()
		windows.SetErrorMode(uint32(mode) | windows.SEM_FAILCRITICALERRORS | windows.SEM_NOGPFAULTERRORBOX)
	}
	if procWerGetFlags.Find() != nil || procWerSetFlags.Find() != nil {
		return
	}
	var flags uint32
	if r, _, _ := procWerGetFlags.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&flags))); int32(r) < 0 {
		return
	}
	procWerSetFlags.Call(uintptr(flags | werFaultReportingFlagNoHeap))
}
