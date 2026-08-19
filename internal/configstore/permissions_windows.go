//go:build windows

package configstore

import "io/fs"

// Windows access control is inherited from the user-owned state directory.
// fs.FileMode cannot represent or validate Windows ACLs.
func secureDirectory(string) error {
	return nil
}

func privateFilePermissions(fs.FileMode) bool {
	return true
}
