//go:build windows

package service

import "os"

// openDefinition opens a definition for reading. Opening a file path on
// Windows does not block waiting for a peer the way a Unix FIFO does.
func openDefinition(path string) (*os.File, error) {
	return os.Open(path)
}
