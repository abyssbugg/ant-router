//go:build !windows

package service

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO at the definition path must be rejected, not opened for a blocking
// read that would hang Install, Uninstall, Start, and Stop forever.
func TestReadOwnedDefinitionRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "com.test.app.plist")
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- readOwnedDefinition(path, launchdOwnerLine) }()
	select {
	case err := <-done:
		var notRegular ErrNotRegularFile
		if !errors.As(err, &notRegular) || notRegular.Path != path {
			t.Fatalf("readOwnedDefinition = %v, want ErrNotRegularFile for %s", err, path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readOwnedDefinition blocked on a FIFO")
	}
}
