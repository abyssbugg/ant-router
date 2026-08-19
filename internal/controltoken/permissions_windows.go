//go:build windows

package controltoken

import (
	"fmt"
	"io/fs"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	tokenKernel32    = syscall.NewLazyDLL("kernel32.dll")
	tokenMoveFileExW = tokenKernel32.NewProc("MoveFileExW")
)

func secureTokenDirectory(path string) error { return applyPrivateWindowsACL(path) }
func privateTokenFile(path string, _ fs.FileMode) bool {
	return applyPrivateWindowsACL(path) == nil
}
func syncTokenDirectory(string) error { return nil }

func applyPrivateWindowsACL(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sddl := fmt.Sprintf("D:P(A;;FA;;;SY)(A;;FA;;;%s)", user.User.Sid.String())
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	)
}

func installTokenFile(source, target string) error {
	err := moveTokenFile(source, target, 0x8)
	if err == syscall.ERROR_FILE_EXISTS || err == syscall.ERROR_ALREADY_EXISTS {
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
