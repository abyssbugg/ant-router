//go:build !windows

package codexintegration

import "os"

func validateDirectorySecurity(_ string, info os.FileInfo) bool { return info.Mode().Perm()&0o077 == 0 }
