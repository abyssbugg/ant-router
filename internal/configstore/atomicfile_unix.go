//go:build !windows

package configstore

import (
	"errors"
	"os"
)

// replaceFile atomically replaces the target file with the temp file using os.Rename.
func replaceFile(tempPath, targetPath string) error {
	return os.Rename(tempPath, targetPath)
}

// syncDirectory syncs the directory by opening it, calling Sync, and closing it.
// Returns errors.Join of sync and close errors, never swallows errors.
func syncDirectory(dirPath string) error {
	dir, err := os.Open(dirPath)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	return errors.Join(syncErr, closeErr)
}
