//go:build windows

package controltoken

import (
	"errors"
	"io/fs"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	tokenKernel32    = syscall.NewLazyDLL("kernel32.dll")
	tokenMoveFileExW = tokenKernel32.NewProc("MoveFileExW")
)

func secureTokenDirectory(string) error { return nil }
func privateTokenFile(fs.FileMode) bool { return true }
func syncTokenDirectory(string) error   { return nil }

func installTokenFile(source, target string) error {
	err := moveTokenFile(source, target, 0x8)
	if errors.Is(err, syscall.ERROR_FILE_EXISTS) || errors.Is(err, syscall.ERROR_ALREADY_EXISTS) {
		return errTokenExists
	}
	return err
}

func replaceTokenFile(source, target string) error {
	return moveTokenFile(source, target, 0x1|0x8)
}

func moveTokenFile(source, target string, flags uintptr) error {
	sourcePointer, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetPointer, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	result, _, callErr := tokenMoveFileExW.Call(
		uintptr(unsafe.Pointer(sourcePointer)),
		uintptr(unsafe.Pointer(targetPointer)),
		flags,
	)
	runtime.KeepAlive(sourcePointer)
	runtime.KeepAlive(targetPointer)
	if result != 0 {
		return nil
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}
