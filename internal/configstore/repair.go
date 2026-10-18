package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/abyssbugg/ant-router/internal/revision"
)

// RepairRevision atomically replaces an existing corrupt revision with bytes
// that hash to the same revision ID.
func (fs *FileStore) RepairRevision(ctx context.Context, id revision.ID, data []byte) error {
	unlock, err := fs.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	if err := contextError(ctx); err != nil {
		return err
	}
	if !id.Valid() || len(data) > MaxRevisionBytes || revision.For(data) != id {
		return &Error{Code: CodeInvalidRevision, Path: "revision_id", Err: fmt.Errorf("repair bytes do not match revision")}
	}
	path := filepath.Join(fs.revisionsDir, id.String()+".toml")
	info, err := os.Lstat(path)
	if err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return &Error{Code: CodeRevisionCorrupt, Path: path, Err: fmt.Errorf("revision is not a regular file")}
	}

	existing, readErr := fs.readFile(path, MaxRevisionBytes)
	if readErr != nil && !errors.Is(readErr, errFileTooLarge) {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: readErr}
	}
	if readErr == nil && privateFilePermissions(path, info.Mode()) && revision.For(existing) == id {
		if bytes.Equal(existing, data) {
			return &Error{Code: CodeRepairUnnecessary, Path: path, Err: fmt.Errorf("revision is already valid")}
		}
		return &Error{Code: CodeRevisionCorrupt, Path: path, Err: fmt.Errorf("revision hash collision")}
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := fs.writeFile(ctx, path, data, 0o600); err != nil {
		return err
	}
	repaired, err := fs.readFile(path, MaxRevisionBytes)
	if err != nil || revision.For(repaired) != id || !bytes.Equal(repaired, data) {
		return &Error{Code: CodeRevisionCorrupt, Path: path, Err: fmt.Errorf("repaired revision verification failed")}
	}
	return nil
}
