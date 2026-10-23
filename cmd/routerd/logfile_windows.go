//go:build windows

package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// privateDescriptor is a protected DACL granting access only to SYSTEM and
// the current user: the Windows equivalent of mode 0600 (the permission
// bits passed to os.OpenFile do not restrict access on Windows).
func privateDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;;FA;;;SY)(A;;FA;;;%s)", user.User.Sid.String()))
}

// createPrivateLogFile opens path for append, creating it if missing with
// the private DACL already in place, so a new log is never readable through
// permissions inherited from its directory, not even briefly.
func createPrivateLogFile(path string) (*os.File, error) {
	descriptor, err := privateDescriptor()
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	attributes := windows.SecurityAttributes{SecurityDescriptor: descriptor}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	handle, err := windows.CreateFile(name, windows.FILE_APPEND_DATA|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		&attributes, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

// openLogFile opens the log privately. Creation-time attributes do not
// apply to an existing file, so its DACL is replaced as well.
func openLogFile(path string) (*os.File, error) {
	file, err := createPrivateLogFile(path)
	if err != nil {
		return nil, err
	}
	descriptor, err := privateDescriptor()
	if err == nil {
		var dacl *windows.ACL
		if dacl, _, err = descriptor.DACL(); err == nil {
			err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
				windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
		}
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure %s: %w", path, err)
	}
	return file, nil
}
