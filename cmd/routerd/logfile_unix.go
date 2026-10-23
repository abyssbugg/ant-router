//go:build !windows

package main

import "os"

// openLogFile opens path for append and makes it owner-only. OpenFile
// applies 0600 only when it creates the file, so an existing file is
// tightened through its descriptor (no path lookup, so no symlink swap
// between open and chmod).
func openLogFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}
