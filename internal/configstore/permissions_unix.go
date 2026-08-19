//go:build !windows

package configstore

import (
	"io/fs"
	"os"
)

func secureDirectory(path string) error {
	return os.Chmod(path, 0o700)
}

func privateFilePermissions(mode fs.FileMode) bool {
	return mode.Perm() == 0o600
}
