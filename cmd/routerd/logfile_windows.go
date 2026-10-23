//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/abyssbugg/ant-router/internal/privateacl"
	"golang.org/x/sys/windows"
)

// createPrivateLogFile opens path for append, creating it if missing with
// the private DACL already in place, so a new log is never readable through
// permissions inherited from its directory, not even briefly.
func createPrivateLogFile(path string) (*os.File, error) {
	descriptor, err := privateacl.FileDescriptor()
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.FILE_APPEND_DATA|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		privateacl.SecurityAttributes(descriptor), windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
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
	descriptor, err := privateacl.FileDescriptor()
	if err == nil {
		err = privateacl.ApplyToPath(path, descriptor)
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure %s: %w", path, err)
	}
	return file, nil
}
