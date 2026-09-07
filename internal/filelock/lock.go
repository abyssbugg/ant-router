package filelock

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
)

// Lock owns one cross-process advisory lock until Release.
type Lock struct {
	once    sync.Once
	release func() error
	err     error
}

func newLock(release func() error) *Lock { return &Lock{release: release} }

// Release unlocks and closes the lock exactly once; concurrent calls are safe.
func (lock *Lock) Release() error {
	if lock == nil {
		return nil
	}
	lock.once.Do(func() {
		if lock.release != nil {
			lock.err = lock.release()
		}
		lock.release = nil
	})
	return lock.err
}

func validate(ctx context.Context, path string) error {
	if ctx == nil || path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." || filepath.Base(path) == string(filepath.Separator) {
		return errors.New("invalid lock request")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
