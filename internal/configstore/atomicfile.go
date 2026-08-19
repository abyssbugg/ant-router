package configstore

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type atomicStage string

const (
	atomicStageCreate        atomicStage = "create"
	atomicStageChmod         atomicStage = "chmod"
	atomicStageWrite         atomicStage = "write"
	atomicStageFileSync      atomicStage = "file_sync"
	atomicStageClose         atomicStage = "close"
	atomicStageReplace       atomicStage = "replace"
	atomicStageDirectorySync atomicStage = "directory_sync"
)

type atomicWriteHooks struct {
	beforeReplace func()
	afterReplace  func()
	syncDirectory func(string) error
	failStage     func(atomicStage) error
}

func writeAtomic(ctx context.Context, path string, data []byte, perm fs.FileMode) error {
	return writeAtomicWithHooks(ctx, path, data, perm, nil)
}

func writeAtomicWithHooks(ctx context.Context, path string, data []byte, perm fs.FileMode, hooks *atomicWriteHooks) error {
	if err := atomicContextError(ctx, path); err != nil {
		return err
	}
	if path == "" {
		return &Error{Code: CodeStoreUnavailable, Err: os.ErrInvalid}
	}
	parent := filepath.Dir(path)
	base := filepath.Base(path)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: parent, Err: err}
	}
	if !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return &Error{Code: CodeStoreUnavailable, Path: parent, Err: os.ErrInvalid}
	}
	if targetInfo, statErr := os.Lstat(path); statErr == nil {
		if targetInfo.IsDir() {
			return &Error{Code: CodeStoreUnavailable, Path: path, Err: os.ErrInvalid}
		}
	} else if !os.IsNotExist(statErr) {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: statErr}
	}
	if err := injectedAtomicFailure(hooks, atomicStageCreate); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}

	tempFile, err := os.CreateTemp(parent, "."+base+".tmp-*")
	if err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	tempPath := tempFile.Name()
	closed := false
	committed := false
	defer func() {
		if !closed {
			_ = tempFile.Close()
		}
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	if err := injectedAtomicFailure(hooks, atomicStageChmod); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	if err := tempFile.Chmod(perm.Perm()); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	if err := injectedAtomicFailure(hooks, atomicStageWrite); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	written, err := io.Copy(tempFile, bytes.NewReader(data))
	if err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	if written != int64(len(data)) {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: io.ErrShortWrite}
	}
	if err := injectedAtomicFailure(hooks, atomicStageFileSync); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	if err := tempFile.Sync(); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	if err := injectedAtomicFailure(hooks, atomicStageClose); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	closeErr := tempFile.Close()
	closed = true
	if closeErr != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: closeErr}
	}

	if hooks != nil && hooks.beforeReplace != nil {
		hooks.beforeReplace()
	}
	if err := atomicContextError(ctx, path); err != nil {
		return err
	}
	if err := injectedAtomicFailure(hooks, atomicStageReplace); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	if err := replaceFile(tempPath, path); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
	}
	committed = true
	if hooks != nil && hooks.afterReplace != nil {
		hooks.afterReplace()
	}
	if err := injectedAtomicFailure(hooks, atomicStageDirectorySync); err != nil {
		return &Error{Code: CodeDurabilityUncertain, Path: parent, Err: err}
	}
	syncDirectoryOperation := syncDirectory
	if hooks != nil && hooks.syncDirectory != nil {
		syncDirectoryOperation = hooks.syncDirectory
	}
	if err := syncDirectoryOperation(parent); err != nil {
		return &Error{Code: CodeDurabilityUncertain, Path: parent, Err: err}
	}
	return nil
}

func injectedAtomicFailure(hooks *atomicWriteHooks, stage atomicStage) error {
	if hooks == nil || hooks.failStage == nil {
		return nil
	}
	return hooks.failStage(stage)
}

func atomicContextError(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return &Error{Code: CodeCancelled, Path: path, Err: ctx.Err()}
	default:
		return nil
	}
}
