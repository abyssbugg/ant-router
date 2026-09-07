package atomicfile

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteWithHooks atomically replaces path and exposes deterministic test boundaries.
func WriteWithHooks(ctx context.Context, path string, data []byte, perm fs.FileMode, hooks *Hooks) (Result, error) {
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	if path == "" {
		return Result{}, os.ErrInvalid
	}
	parent, base := filepath.Dir(path), filepath.Base(path)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return Result{}, err
	}
	if !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return Result{}, os.ErrInvalid
	}
	if targetInfo, statErr := os.Lstat(path); statErr == nil {
		if targetInfo.IsDir() {
			return Result{}, os.ErrInvalid
		}
	} else if !os.IsNotExist(statErr) {
		return Result{}, statErr
	}
	if err := fail(hooks, StageCreate); err != nil {
		return Result{}, err
	}
	tempFile, err := os.CreateTemp(parent, "."+base+".tmp-*")
	if err != nil {
		return Result{}, err
	}
	tempPath := tempFile.Name()
	closed, committed := false, false
	defer func() {
		if !closed {
			_ = tempFile.Close()
		}
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := fail(hooks, StageChmod); err != nil {
		return Result{}, err
	}
	if err := tempFile.Chmod(perm.Perm()); err != nil {
		return Result{}, err
	}
	if err := fail(hooks, StageWrite); err != nil {
		return Result{}, err
	}
	written, err := io.Copy(tempFile, bytes.NewReader(data))
	if err != nil {
		return Result{}, err
	}
	if written != int64(len(data)) {
		return Result{}, io.ErrShortWrite
	}
	if err := fail(hooks, StageFileSync); err != nil {
		return Result{}, err
	}
	if err := tempFile.Sync(); err != nil {
		return Result{}, err
	}
	if err := fail(hooks, StageClose); err != nil {
		return Result{}, err
	}
	if err := tempFile.Close(); err != nil {
		closed = true
		return Result{}, err
	}
	closed = true
	if hooks != nil && hooks.BeforeReplace != nil {
		hooks.BeforeReplace()
	}
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	if err := fail(hooks, StageReplace); err != nil {
		return Result{}, err
	}
	if err := replaceFile(tempPath, path); err != nil {
		return Result{}, err
	}
	committed = true
	result := Result{Committed: true}
	if hooks != nil && hooks.AfterReplace != nil {
		hooks.AfterReplace()
	}
	if err := fail(hooks, StageDirectorySync); err != nil {
		return result, err
	}
	syncOperation := syncDirectory
	if hooks != nil && hooks.SyncDirectory != nil {
		syncOperation = hooks.SyncDirectory
	}
	if err := syncOperation(parent); err != nil {
		return result, err
	}
	return result, nil
}
