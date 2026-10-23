package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/daemon"
)

type fakeRunner struct{ err error }

func (r fakeRunner) Run(context.Context) error { return r.err }

func TestRunParsesOptionsAndRunsDaemon(t *testing.T) {
	var captured daemon.Options
	factory := func(options daemon.Options) (daemonRunner, error) {
		captured = options
		return fakeRunner{}, nil
	}
	var stderr strings.Builder
	code := run(context.Background(), []string{"--instance", "dev", "--state-dir", "state", "--runtime-dir", "runtime"}, io.Discard, &stderr, factory)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("run() = %d stderr %q", code, stderr.String())
	}
	if captured.Instance != "dev" || captured.StateRoot != "state" || captured.RuntimeRoot != "runtime" || captured.Build.Version == "" {
		t.Errorf("captured options = %#v", captured)
	}
}

func TestRunUsageAndHelp(t *testing.T) {
	var stderr strings.Builder
	factory := func(daemon.Options) (daemonRunner, error) { t.Fatal("factory called"); return nil, nil }
	if code := run(context.Background(), []string{"unexpected"}, io.Discard, &stderr, factory); code != 2 {
		t.Errorf("usage exit = %d, want 2", code)
	}
	stderr.Reset()
	if code := run(context.Background(), []string{"--help"}, io.Discard, &stderr, factory); code != 0 {
		t.Errorf("help exit = %d, want 0", code)
	}
}

func TestRunStartupAndRuntimeFailures(t *testing.T) {
	sentinel := errors.New("synthetic startup failure")
	var stderr strings.Builder
	if code := run(context.Background(), nil, io.Discard, &stderr, func(daemon.Options) (daemonRunner, error) { return nil, sentinel }); code != 1 || !strings.Contains(stderr.String(), "startup failed") {
		t.Errorf("startup failure = %d %q", code, stderr.String())
	}
	stderr.Reset()
	if code := run(context.Background(), nil, io.Discard, &stderr, func(daemon.Options) (daemonRunner, error) { return fakeRunner{err: sentinel}, nil }); code != 1 || !strings.Contains(stderr.String(), "daemon failed") {
		t.Errorf("runtime failure = %d %q", code, stderr.String())
	}
}

func TestRunCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code := run(ctx, nil, io.Discard, io.Discard, func(daemon.Options) (daemonRunner, error) { return fakeRunner{}, nil })
	if code != 0 {
		t.Errorf("cancelled run = %d, want 0", code)
	}
}

// --log-file appends the daemon's diagnostics to the named file instead of
// stderr, for service managers (Task Scheduler) that cannot redirect output.
func TestRunLogFileReceivesDiagnostics(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "routerd.log")
	if err := os.WriteFile(logPath, []byte("previous\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	failing := func(daemon.Options) (daemonRunner, error) { return fakeRunner{err: errors.New("synthetic")}, nil }
	if code := run(context.Background(), []string{"--log-file", logPath}, io.Discard, &stderr, failing); code != 1 {
		t.Fatalf("run() = %d, want 1", code)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "previous\n") || !strings.Contains(string(data), "daemon failed: synthetic") || stderr.Len() != 0 {
		t.Fatalf("log = %q stderr = %q, want appended diagnostics and empty stderr", data, stderr.String())
	}
}

func TestRunLogFileOpenFailure(t *testing.T) {
	var stderr strings.Builder
	factory := func(daemon.Options) (daemonRunner, error) { t.Fatal("factory called"); return nil, nil }
	missingDir := filepath.Join(t.TempDir(), "missing", "routerd.log")
	if code := run(context.Background(), []string{"--log-file", missingDir}, io.Discard, &stderr, factory); code != 1 || !strings.Contains(stderr.String(), "open log file") {
		t.Fatalf("run() = %d stderr %q, want 1 and an open error", code, stderr.String())
	}
	if code := run(context.Background(), []string{"--log-file", "relative.log"}, io.Discard, &stderr, factory); code != 2 {
		t.Fatalf("relative log file exit = %d, want 2", code)
	}
}
