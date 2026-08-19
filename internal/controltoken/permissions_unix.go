//go:build !windows

package controltoken

import (
	"errors"
	"io/fs"
	"os"
)

func secureTokenDirectory(path string) error { return os.Chmod(path, 0o700) }
func privateTokenFile(mode fs.FileMode) bool { return mode.Perm() == 0o600 }
func installTokenFile(source, target string) error {
	if err := os.Link(source, target); err != nil {
		if os.IsExist(err) {
			return errTokenExists
		}
		return err
	}
	return nil
}
func replaceTokenFile(source, target string) error { return os.Rename(source, target) }
func syncTokenDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	return errors.Join(syncErr, closeErr)
}
