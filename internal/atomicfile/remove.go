package atomicfile

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
)

// Remove durably removes a regular file and reports whether unlink committed.
// Callers must serialize mutations in the target directory across processes.
func Remove(ctx context.Context, path string) (Result, error) { return RemoveWithHooks(ctx, path, nil) }

// RemoveWithHooks removes a regular file with deterministic fault boundaries.
func RemoveWithHooks(ctx context.Context, path string, hooks *Hooks) (Result, error) {
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Result{}, os.ErrInvalid
	}
	parent := filepath.Dir(path)
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return Result{}, os.ErrInvalid
	}
	targetInfo, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	if !targetInfo.Mode().IsRegular() || targetInfo.Mode()&os.ModeSymlink != 0 || targetInfo.Mode()&fs.ModeType != 0 {
		return Result{}, os.ErrInvalid
	}
	if hooks != nil && hooks.BeforeReplace != nil {
		hooks.BeforeReplace()
	}
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	if err := fail(hooks, StageReplace); err != nil {
		return Result{}, err
	}
	if err := removeFile(path); err != nil {
		return Result{}, err
	}
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
