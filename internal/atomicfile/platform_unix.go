//go:build !windows

package atomicfile

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func replaceFile(tempPath, targetPath string) error { return os.Rename(tempPath, targetPath) }

func removeFile(path string) error {
	parentFD, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, filepath.Base(path), &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return os.ErrInvalid
	}
	return unix.Unlinkat(parentFD, filepath.Base(path), 0)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	return errors.Join(syncErr, closeErr)
}
