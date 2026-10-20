//go:build !windows

package service

import (
	"os"
	"syscall"
)

// openDefinition opens a definition without blocking. O_NONBLOCK makes
// opening a FIFO return immediately instead of waiting for a writer; it has
// no effect on reads from a regular file, which is all the caller accepts.
func openDefinition(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
