//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"github.com/abyssbugg/ant-router/internal/privateacl"
)

// ensurePrivateLogDir creates logDir (and any missing directory between
// stateRoot and logDir) with an owner-only, inheritable DACL.
//
// Each component below the state root is opened without following reparse
// points and kept open, without FILE_SHARE_DELETE, until the walk finishes, so
// no component can be renamed or replaced by a junction or symlink while it
// is checked. Every component must be a real directory, and each receives the
// private DACL whether it was just created or already existed.
func ensurePrivateLogDir(stateRoot, logDir string) error {
	components, err := logDirComponents(stateRoot, logDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return err
	}
	sd, err := privateacl.DirectoryDescriptor()
	if err != nil {
		return err
	}
	var handles []windows.Handle
	defer func() {
		for _, handle := range handles {
			_ = windows.CloseHandle(handle)
		}
	}()
	root, err := openDirectoryHandle(stateRoot, false)
	if err != nil {
		return err
	}
	handles = append(handles, root)
	current := stateRoot
	for _, name := range components {
		current = filepath.Join(current, name)
		path, err := windows.UTF16PtrFromString(current)
		if err != nil {
			return err
		}
		if err := windows.CreateDirectory(path, privateacl.SecurityAttributes(sd)); err != nil &&
			!errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return fmt.Errorf("create %s: %w", current, err)
		}
		handle, err := openDirectoryHandle(current, true)
		if err != nil {
			return err
		}
		handles = append(handles, handle)
		if err := privateacl.ApplyToHandle(handle, sd); err != nil {
			return fmt.Errorf("restrict %s: %w", current, err)
		}
	}
	return nil
}

// openDirectoryHandle opens path as a directory. With noReparse it opens the
// reparse point itself and refuses it, rather than following it.
func openDirectoryHandle(path string, noReparse bool) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	flags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if noReparse {
		flags |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}
	handle, err := windows.CreateFile(name,
		windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("open %s: %w", path, err)
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return windows.InvalidHandle, fmt.Errorf("inspect %s: %w", path, err)
	}
	if noReparse && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return windows.InvalidHandle, fmt.Errorf("%s is a reparse point", path)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		_ = windows.CloseHandle(handle)
		return windows.InvalidHandle, fmt.Errorf("%s is not a directory", path)
	}
	return handle, nil
}
