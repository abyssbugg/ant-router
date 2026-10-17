package service

import (
	"context"
	"os/exec"
)

const maxOutputSize = 64 * 1024 // 64 KiB

// ExecRunner implements Runner using os/exec.
type ExecRunner struct{}

// Run executes a command with bounded output (64 KiB per stream).
func (r *ExecRunner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, name, args...)

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
