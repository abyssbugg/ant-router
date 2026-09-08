// Package atomicfile provides durable same-directory file replacement.
package atomicfile

import (
	"context"
	"io/fs"
)

// Stage identifies an injectable atomic-write boundary.
type Stage string

const (
	StageCreate        Stage = "create"
	StageChmod         Stage = "chmod"
	StageWrite         Stage = "write"
	StageFileSync      Stage = "file_sync"
	StageClose         Stage = "close"
	StageReplace       Stage = "replace"
	StageDirectorySync Stage = "directory_sync"
)

// Hooks customize atomic boundaries for deterministic fault tests.
type Hooks struct {
	BeforeReplace func()
	AfterReplace  func()
	SyncDirectory func(string) error
	FailStage     func(Stage) error
}

// Result reports whether the target replacement committed.
type Result struct{ Committed bool }

// Write atomically replaces path with data and reports commit state.
func Write(ctx context.Context, path string, data []byte, perm fs.FileMode) (Result, error) {
	return WriteWithHooks(ctx, path, data, perm, nil)
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func fail(hooks *Hooks, stage Stage) error {
	if hooks == nil || hooks.FailStage == nil {
		return nil
	}
	return hooks.FailStage(stage)
}
