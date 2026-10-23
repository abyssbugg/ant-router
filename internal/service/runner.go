package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const maxOutputSize = 64 * 1024 // 64 KiB

// ExecRunner implements Runner using os/exec.
type ExecRunner struct{}

// Run executes launchctl, systemctl, or the system Windows PowerShell with
// bounded output (64 KiB per stream).
func (r *ExecRunner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	cmd, err := serviceCommand(ctx, name, args...)
	if err != nil {
		return nil, nil, err
	}
	return runBounded(cmd)
}

// serviceCommand is the only place this package builds an *exec.Cmd. The
// program is always fixed (launchctl, systemctl, or the system copy of
// Windows PowerShell) so ExecRunner can never become a general command
// runner; arguments are passed as a vector and never through a shell.
func serviceCommand(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	switch name {
	case "launchctl":
		return exec.CommandContext(ctx, "launchctl", args...), nil
	case "systemctl":
		return exec.CommandContext(ctx, "systemctl", args...), nil
	case "powershell":
		// Resolved from SystemRoot, not PATH, so a planted powershell.exe
		// earlier in PATH is never run.
		root := os.Getenv("SystemRoot")
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("SystemRoot %q is not an absolute path", root)
		}
		return exec.CommandContext(ctx, filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), args...), nil
	default:
		return nil, fmt.Errorf("command %q is not permitted", name)
	}
}

// runBounded runs cmd, keeping at most maxOutputSize bytes of each stream.
func runBounded(cmd *exec.Cmd) (stdout, stderr []byte, err error) {
	// Use a bounded writer for each stream; excess is discarded.
	outBuf := &boundedBuffer{limit: maxOutputSize}
	errBuf := &boundedBuffer{limit: maxOutputSize}

	cmd.Stdout = outBuf
	cmd.Stderr = errBuf

	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// boundedBuffer discards writes beyond its limit.
type boundedBuffer struct {
	data  []byte
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(b.data) >= b.limit {
		return len(p), nil // discard
	}
	remaining := b.limit - len(b.data)
	if len(p) <= remaining {
		b.data = append(b.data, p...)
		return len(p), nil
	}
	b.data = append(b.data, p[:remaining]...)
	return len(p), nil
}

func (b *boundedBuffer) Bytes() []byte {
	return b.data
}
