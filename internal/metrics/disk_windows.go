//go:build windows

package metrics

import (
	"syscall"
	"unsafe"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpace = kernel32.NewProc("GetDiskFreeSpaceExW")
)

func getDiskSpace() (freeBytes, totalBytes uint64) {
	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes int64
	dir, err := syscall.UTF16PtrFromString(".")
	if err != nil {
		return 0, 0
	}
	r1, _, _ := procGetDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(dir)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)
	if r1 == 0 {
		return 0, 0
	}
	return uint64(freeBytesAvailable), uint64(totalNumberOfBytes)
}
