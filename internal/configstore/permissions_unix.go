//go:build !windows

package configstore

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

func secureDirectory(path string) error                      { return os.Chmod(path, 0o700) }
func privateFilePermissions(_ string, mode fs.FileMode) bool { return mode.Perm() == 0o600 }
func openManagedFile(path string) (*os.File, error) {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), path), nil
}
