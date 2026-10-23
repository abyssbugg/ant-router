//go:build !windows

package main

// securePrivateFile is a no-op where the 0600 mode given to OpenFile already
// makes a newly created log owner-only.
func securePrivateFile(string) error { return nil }
