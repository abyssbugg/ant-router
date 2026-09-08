//go:build windows

package codexintegration

import "os"

func validateDirectorySecurity(_ string, _ os.FileInfo) bool { return true }
