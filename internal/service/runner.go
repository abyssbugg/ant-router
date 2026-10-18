package service

import (
	"context"
	"fmt"
	"os/exec"
)

const maxOutputSize = 64 * 1024 // 64 KiB

// ExecRunner implements Runner using os/exec.
type ExecRunner struct{}

// permittedCommands are the only programs the service backends run. Fixing
// the program name keeps ExecRunner from becoming a general command runner;
// arguments are passed as a vector and never through a shell.
var permittedCommands = map[string]bool{"launchctl": true, "systemctl": true}

// Run executes launchctl or systemctl with bounded output (64 KiB per stream).
func (r *ExecRunner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	if !permittedCommands[name] {
		return nil, nil, fmt.Errorf("command %q is not permitted", name)
	}
	return runBounded(ctx, name, args...)
}

func runBounded(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- name is from permittedCommands

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
