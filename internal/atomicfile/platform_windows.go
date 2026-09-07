//go:build windows

package atomicfile

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	kernel32    = syscall.NewLazyDLL("kernel32.dll")
	moveFileExW = kernel32.NewProc("MoveFileExW")
)

func replaceFile(tempPath, targetPath string) error {
	oldName, err := syscall.UTF16PtrFromString(tempPath)
	if err != nil {
		return err
	}
	newName, err := syscall.UTF16PtrFromString(targetPath)
	if err != nil {
		return err
	}
	const replaceExisting = 0x1
	const writeThrough = 0x8
	result, _, callErr := moveFileExW.Call(uintptr(unsafe.Pointer(oldName)), uintptr(unsafe.Pointer(newName)), replaceExisting|writeThrough)
	runtime.KeepAlive(oldName)
	runtime.KeepAlive(newName)
	if result == 0 {
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return syscall.EINVAL
	}
	return nil
}

func syncDirectory(string) error { return nil }
