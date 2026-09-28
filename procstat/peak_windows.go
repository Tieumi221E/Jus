package procstat

import (
	"syscall"
	"unsafe"
)

// PeakWorkingSet is the most memory the process has had resident, in bytes (0 if unknown).
func PeakWorkingSet() uint64 {
	type counters struct {
		Cb                         uint32
		PageFaultCount             uint32
		PeakWorkingSetSize         uintptr
		WorkingSetSize             uintptr
		QuotaPeakPagedPoolUsage    uintptr
		QuotaPagedPoolUsage        uintptr
		QuotaPeakNonPagedPoolUsage uintptr
		QuotaNonPagedPoolUsage     uintptr
		PagefileUsage              uintptr
		PeakPagefileUsage          uintptr
	}
	var c counters
	c.Cb = uint32(unsafe.Sizeof(c))
	h, _ := syscall.GetCurrentProcess()
	proc := syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	if r, _, _ := proc.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.Cb)); r == 0 {
		return 0
	}
	return uint64(c.PeakWorkingSetSize)
}
