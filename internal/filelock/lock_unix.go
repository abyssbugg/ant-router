//go:build !windows

package filelock

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// Acquire waits for an exclusive Unix flock until ctx ends; it has no implicit timeout.
func Acquire(ctx context.Context, path string) (*Lock, error) {
	if err := validate(ctx, path); err != nil {
		return nil, err
	}
	parentFD, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("lock unavailable")
	}
	defer unix.Close(parentFD)
	fd, err := unix.Openat(parentFD, filepath.Base(path), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, errors.New("lock unavailable")
	}
	closeFD := true
	defer func() {
		if closeFD {
			_ = unix.Close(fd)
		}
	}()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || unix.Fchmod(fd, 0o600) != nil {
		return nil, errors.New("lock unavailable")
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return nil, errors.New("lock unavailable")
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	closeFD = false
	return newLock(func() error {
		unlockErr := unix.Flock(fd, unix.LOCK_UN)
		closeErr := unix.Close(fd)
		return errors.Join(unlockErr, closeErr)
	}), nil
}
