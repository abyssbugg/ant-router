//go:build !windows

package codexprocess

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
)

func runCommand(ctx context.Context, command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		killErr := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		waitErr := <-done
		if killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			return killErr
		}
		_ = waitErr
		return ctx.Err()
	}
}
