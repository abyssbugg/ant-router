//go:build windows

package configstore

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	kernel32    = syscall.NewLazyDLL("kernel32.dll")
	moveFileExW = kernel32.NewProc("MoveFileExW")
)

// replaceFile atomically replaces the target file with the temp file using MoveFileExW.
// It uses flags 0x1 (MOVEFILE_REPLACE_EXISTING) | 0x8 (MOVEFILE_WRITE_THROUGH).
func replaceFile(tempPath, targetPath string) error {
	lpExistingFileName, err := syscall.UTF16PtrFromString(tempPath)
	if err != nil {
		return err
	}

	lpNewFileName, err := syscall.UTF16PtrFromString(targetPath)
	if err != nil {
		return err
	}

	const (
		MOVEFILE_REPLACE_EXISTING = 0x1
		MOVEFILE_WRITE_THROUGH    = 0x8
	)

	ret, _, err := moveFileExW.Call(
		uintptr(unsafe.Pointer(lpExistingFileName)),
		uintptr(unsafe.Pointer(lpNewFileName)),
		MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH,
	)
	runtime.KeepAlive(lpExistingFileName)
	runtime.KeepAlive(lpNewFileName)

	if ret == 0 {
		if err != syscall.Errno(0) {
			return err
		}
		return syscall.EINVAL
	}

	return nil
}

// syncDirectory is a no-op on Windows because WRITE_THROUGH flag handles durability.
func syncDirectory(dirPath string) error {
	return nil
}
