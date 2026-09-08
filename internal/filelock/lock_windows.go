//go:build windows

package filelock

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// Acquire shares the file handle for contenders but holds an exclusive LockFileEx range until ctx ends.
func Acquire(ctx context.Context, path string) (*Lock, error) {
	if err := validate(ctx, path); err != nil {
		return nil, err
	}
	parent, err := windows.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return nil, errors.New("lock unavailable")
	}
	attributes, err := windows.GetFileAttributes(parent)
	if err != nil || attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, errors.New("lock unavailable")
	}
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, errors.New("lock unavailable")
	}
	handle, err := windows.CreateFile(pathPointer, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, errors.New("lock unavailable")
	}
	closeHandle := true
	defer func() {
		if closeHandle {
			_ = windows.CloseHandle(handle)
		}
	}()
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &info) != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return nil, errors.New("lock unavailable")
	}
	overlapped := &windows.Overlapped{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err = windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, errors.New("lock unavailable")
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	closeHandle = false
	return newLock(func() error {
		unlockErr := windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
		closeErr := windows.CloseHandle(handle)
		return errors.Join(unlockErr, closeErr)
	}), nil
}
