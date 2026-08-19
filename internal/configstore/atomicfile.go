package configstore

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// atomicWriteHooks provides hooks called during atomic write operations
type atomicWriteHooks struct {
	beforeReplace func()
	afterReplace  func()
	syncDirectory func(string) error
}

// writeAtomic atomically writes data to a file, creating it if it doesn't exist or replacing it if it does.
// It delegates to writeAtomicWithHooks with zero hooks.
func writeAtomic(ctx context.Context, path string, data []byte, perm fs.FileMode) error {
	return writeAtomicWithHooks(ctx, path, data, perm, nil)
}

// writeAtomicWithHooks atomically writes data to a file with optional hooks.
// The implementation follows this flow:
// 1. Validate context and path
// 2. Validate parent directory (must exist, be a directory, not be a symlink)
// 3. Validate target (reject if directory; allow not-exist and symlink)
// 4. Create temp file in same parent with hidden name .<base>.tmp-*
// 5. Write data to temp file and verify byte count
// 6. Sync and close temp file
// 7. Call beforeReplace hook
// 8. Check context once before replace (clean temp on cancellation)
// 9. Call platform-specific replace
// 10. Mark replacement committed
// 11. Call afterReplace hook (do not check context after commit)
// 12. Call syncDirectory
func writeAtomicWithHooks(ctx context.Context, path string, data []byte, perm fs.FileMode, hooks *atomicWriteHooks) error {
	// Validate context early
	select {
	case <-ctx.Done():
		return &Error{Code: CodeCancelled, Path: path, Err: ctx.Err()}
	default:
	}

	// Validate path
	if path == "" {
		return &Error{Code: CodeStoreUnavailable, Err: os.ErrInvalid}
	}

	// Get parent directory and base filename
	parent := filepath.Dir(path)
	base := filepath.Base(path)

	// Validate parent directory exists, is a directory, and is not a symlink
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return &Error{Code: CodeStoreUnavailable, Path: parent, Err: err}
		}
		return &Error{Code: CodeStoreUnavailable, Path: parent, Err: err}
	}
	if !parentInfo.IsDir() {
		return &Error{Code: CodeStoreUnavailable, Path: parent, Err: os.ErrNotExist}
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 {
		return &Error{Code: CodeStoreUnavailable, Path: parent, Err: os.ErrInvalid}
	}

	// Validate target (reject if directory; allow not-exist and symlink)
	targetInfo, err := os.Lstat(path)
	if err == nil {
		// File/symlink exists
		if targetInfo.IsDir() {
			return &Error{Code: CodeStoreUnavailable, Path: path, Err: os.ErrInvalid}
		}
		// Allow symlink and regular file to be replaced
	} else if !os.IsNotExist(err) {
		// Some other error occurred
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	// If not-exist, that's fine

	// Create temp file in same parent with hidden name
	tempFile, err := os.CreateTemp(parent, "."+base+".tmp-*")
	if err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	tempPath := tempFile.Name()
	committed := false

	// Defer cleanup: remove temp file only if not yet committed
	defer func() {
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	// Set permissions on temp file
	if err := tempFile.Chmod(perm.Perm()); err != nil {
		_ = tempFile.Close()
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}

	// Write data to temp file using io.Copy from bytes.NewReader
	n, err := io.Copy(tempFile, bytes.NewReader(data))
	if err != nil {
		_ = tempFile.Close()
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}

	// Verify byte count
	if n != int64(len(data)) {
		_ = tempFile.Close()
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: io.ErrShortWrite}
	}

	// Sync temp file
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}

	// Close temp file
	if err := tempFile.Close(); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}

	// Call beforeReplace hook
	if hooks != nil && hooks.beforeReplace != nil {
		hooks.beforeReplace()
	}

	// Check context once immediately before replace
	select {
	case <-ctx.Done():
		return &Error{Code: CodeCancelled, Path: path, Err: ctx.Err()}
	default:
	}

	// Call platform-specific replace
	if err := replaceFile(tempPath, path); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}

	// Mark replacement committed
	committed = true

	// Call afterReplace hook (do not check context after commit)
	if hooks != nil && hooks.afterReplace != nil {
		hooks.afterReplace()
	}

	// Sync directory - use hook if provided, otherwise use default syncDirectory
	var syncErr error
	if hooks != nil && hooks.syncDirectory != nil {
		syncErr = hooks.syncDirectory(parent)
	} else {
		syncErr = syncDirectory(parent)
	}
	if syncErr != nil {
		return &Error{Code: CodeDurabilityUncertain, Path: parent, Err: syncErr}
	}

	return nil
}
