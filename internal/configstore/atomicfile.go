package configstore

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/abyssbugg/ant-router/internal/atomicfile"
)

type atomicStage = atomicfile.Stage

const (
	atomicStageCreate        = atomicfile.StageCreate
	atomicStageChmod         = atomicfile.StageChmod
	atomicStageWrite         = atomicfile.StageWrite
	atomicStageFileSync      = atomicfile.StageFileSync
	atomicStageClose         = atomicfile.StageClose
	atomicStageReplace       = atomicfile.StageReplace
	atomicStageDirectorySync = atomicfile.StageDirectorySync
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
	var sharedHooks *atomicfile.Hooks
	if hooks != nil {
		sharedHooks = &atomicfile.Hooks{BeforeReplace: hooks.beforeReplace, AfterReplace: hooks.afterReplace, SyncDirectory: hooks.syncDirectory, FailStage: hooks.failStage}
	}
	result, err := atomicfile.WriteWithHooks(ctx, path, data, perm, sharedHooks)
	if err == nil {
		return nil
	}
	code := CodeStoreUnavailable
	errorPath := path
	if result.Committed {
		code = CodeDurabilityUncertain
		errorPath = filepath.Dir(path)
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = CodeCancelled
	}
	return &Error{Code: code, Path: errorPath, Err: err}
}
