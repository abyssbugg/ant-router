//go:build !windows

package main

import "os"

// securePrivateFile makes the open log owner-only. OpenFile applies 0600
// only when it creates the file, so an existing file is tightened through
// its descriptor (no path lookup, so no symlink swap between open and chmod).
func securePrivateFile(file *os.File) error { return file.Chmod(0o600) }
