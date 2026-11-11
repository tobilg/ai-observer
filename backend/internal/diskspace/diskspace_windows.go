//go:build windows

package diskspace

import (
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

func available(dir string) (uint64, error) {
	path, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var freeToCaller, total, free uint64
	r, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&free)),
	)
	if r == 0 {
		return 0, callErr
	}
	return freeToCaller, nil
}
