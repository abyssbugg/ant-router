//go:build !windows

package atomicfile

import (
	"errors"
	"os"
)

func replaceFile(tempPath, targetPath string) error { return os.Rename(tempPath, targetPath) }

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	return errors.Join(syncErr, closeErr)
}
