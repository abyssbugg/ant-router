//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// ensurePrivateLogDir creates logDir (and any missing directory between
// stateRoot and logDir) as a private, owner-only real directory.
//
// The state root itself is the user's choice and may be reached through a
// symlink. Every component below it is opened relative to its already-open
// parent with O_NOFOLLOW, so a symlink planted at, or swapped into, any
// component is refused instead of redirecting the service log elsewhere. Each
// component must be owned by the effective user and is tightened to 0700.
func ensurePrivateLogDir(stateRoot, logDir string) error {
	components, err := logDirComponents(stateRoot, logDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return err
	}
	fd, err := unix.Open(stateRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open state directory: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()
	if err := checkOwnedDirectory(fd, stateRoot, false); err != nil {
		return err
	}
	current := stateRoot
	for _, name := range components {
		current = current + string(os.PathSeparator) + name
		if err := unix.Mkdirat(fd, name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("create %s: %w", current, err)
		}
		next, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
				return fmt.Errorf("%s is not a real directory", current)
			}
			return fmt.Errorf("open %s: %w", current, err)
		}
		_ = unix.Close(fd)
		fd = next
		if err := checkOwnedDirectory(fd, current, true); err != nil {
			return err
		}
	}
	return nil
}

// checkOwnedDirectory requires fd to be a directory owned by the effective
// user and, when tighten is set, sets its mode to exactly 0700.
func checkOwnedDirectory(fd int, path string, tighten bool) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("%s is not a directory", path)
	}
	if int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("%s is owned by another user", path)
	}
	// Compare the whole mode, so setuid, setgid, and sticky bits are
	// cleared as well as group and other permissions.
	if tighten && st.Mode&0o7777 != 0o700 {
		if err := unix.Fchmod(fd, 0o700); err != nil {
			return fmt.Errorf("restrict %s: %w", path, err)
		}
	}
	return nil
}
