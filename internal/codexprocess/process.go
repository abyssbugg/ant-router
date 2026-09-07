// Package codexprocess runs Codex directly without a shell.
package codexprocess

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/abyssbugg/ant-router/internal/codexversion"
)

type Runner struct {
	path                string
	prefix, environment []string
}
type RunOptions struct {
	CodexHome, Profile string
	Args               []string
	Stdin              io.Reader
	Stdout, Stderr     io.Writer
}
type Result struct{ ExitCode int }

func New(path string, prefix, environment []string) (*Runner, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return nil, errors.New("invalid Codex executable")
	}
	for _, value := range append(append([]string(nil), prefix...), environment...) {
		if strings.ContainsRune(value, 0) {
			return nil, errors.New("invalid Codex process option")
		}
	}
	return &Runner{path: path, prefix: append([]string(nil), prefix...), environment: append([]string(nil), environment...)}, nil
}

func validProfile(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for index, char := range value {
		if !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') && !(char == '-' && index > 0) {
			return false
		}
	}
	return value[len(value)-1] != '-'
}

func (runner *Runner) Version(ctx context.Context, allowUntested bool) (codexversion.Version, error) {
	if runner == nil || ctx == nil {
		return codexversion.Version{}, errors.New("invalid Codex version request")
	}
	var stdout boundedBuffer
	stdout.limit = 4096
	command := exec.Command(runner.path, append(append([]string(nil), runner.prefix...), "--version")...)
	command.Env = runner.environmentFor("")
	command.Stdout, command.Stderr = &stdout, io.Discard
	if err := runCommand(ctx, command); err != nil {
		if ctx.Err() != nil {
			return codexversion.Version{}, ctx.Err()
		}
		return codexversion.Version{}, errors.New("Codex version command failed")
	}
	return codexversion.Check(stdout.String(), allowUntested)
}

func (runner *Runner) Run(ctx context.Context, options RunOptions) (Result, error) {
	if runner == nil || ctx == nil || !filepath.IsAbs(options.CodexHome) || !validProfile(options.Profile) {
		return Result{}, errors.New("invalid Codex run request")
	}
	args := append(append([]string(nil), runner.prefix...), "--profile", options.Profile)
	args = append(args, options.Args...)
	command := exec.Command(runner.path, args...)
	command.Env = runner.environmentFor(options.CodexHome)
	command.Stdin = options.Stdin
	command.Stdout, command.Stderr = options.Stdout, options.Stderr
	if command.Stdout == nil {
		command.Stdout = io.Discard
	}
	if command.Stderr == nil {
		command.Stderr = io.Discard
	}
	err := runCommand(ctx, command)
	if err == nil {
		return Result{ExitCode: 0}, nil
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return Result{ExitCode: exitError.ExitCode()}, nil
	}
	return Result{}, errors.New("Codex process failed")
}

func (runner *Runner) environmentFor(codexHome string) []string {
	values := append([]string(nil), os.Environ()...)
	for _, value := range runner.environment {
		values = setEnvironment(values, value)
	}
	if codexHome != "" {
		values = setEnvironment(values, "CODEX_HOME="+codexHome)
	}
	return values
}

func setEnvironment(values []string, replacement string) []string {
	key, _, _ := strings.Cut(replacement, "=")
	result := values[:0]
	for _, value := range values {
		if current, _, _ := strings.Cut(value, "="); current != key {
			result = append(result, value)
		}
	}
	return append(result, replacement)
}

type boundedBuffer struct {
	data  []byte
	limit int
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	if len(buffer.data)+len(data) > buffer.limit {
		return 0, errors.New("Codex process output exceeded limit")
	}
	buffer.data = append(buffer.data, data...)
	return len(data), nil
}
func (buffer *boundedBuffer) String() string { return string(buffer.data) }
