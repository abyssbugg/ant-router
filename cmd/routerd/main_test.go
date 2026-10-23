package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
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
	// The parent "directory" is a regular file, so neither it nor the log
	// file can be created.
	parent := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"--log-file", filepath.Join(parent, "routerd.log")}, io.Discard, &stderr, factory); code != 1 || !strings.Contains(stderr.String(), "create log directory") {
		t.Fatalf("run() = %d stderr %q, want 1 and a create-log-directory error", code, stderr.String())
	}
	// The log path itself is a directory: the directory step succeeds and
	// opening the file fails.
	stderr.Reset()
	if code := run(context.Background(), []string{"--log-file", t.TempDir()}, io.Discard, &stderr, factory); code != 1 || !strings.Contains(stderr.String(), "open log file") {
		t.Fatalf("run() = %d stderr %q, want 1 and an open-log-file error", code, stderr.String())
	}
	if code := run(context.Background(), []string{"--log-file", "relative.log"}, io.Discard, &stderr, factory); code != 2 {
		t.Fatalf("relative log file exit = %d, want 2", code)
	}
}

type loggingRunner struct{}

func (loggingRunner) Run(context.Context) error {
	log.Print("standard logger diagnostic")
	return nil
}

// A fresh service install has no log directory yet: routerd creates it,
// creates the log owner-only, and routes the standard logger (which
// http.Server uses when ErrorLog is nil) into the file for the run only.
func TestRunLogFileCreatesFileAndCapturesStandardLogger(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "logs", "nested", "routerd.log")
	previous := log.Writer()
	var stderr strings.Builder
	factory := func(daemon.Options) (daemonRunner, error) { return loggingRunner{}, nil }
	if code := run(context.Background(), []string{"--log-file", logPath}, io.Discard, &stderr, factory); code != 0 {
		t.Fatalf("run() = %d stderr %q, want 0", code, stderr.String())
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("log mode = %v, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "standard logger diagnostic") || stderr.Len() != 0 {
		t.Fatalf("log = %q stderr = %q, want the standard logger in the log file", data, stderr.String())
	}
	if log.Writer() != previous {
		t.Fatal("run did not restore the standard logger output")
	}
}

// An existing log file with broader permissions is tightened to owner-only
// before routerd appends to it.
func TestRunLogFileTightensExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows access is governed by the ACL; see TestRunLogFileIsPrivateOnWindows")
	}
	logPath := filepath.Join(t.TempDir(), "routerd.log")
	if err := os.WriteFile(logPath, []byte("previous\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(logPath, 0o644); err != nil {
		t.Fatal(err)
	}
	factory := func(daemon.Options) (daemonRunner, error) { return fakeRunner{}, nil }
	if code := run(context.Background(), []string{"--log-file", logPath}, io.Discard, io.Discard, factory); code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("log mode = %v, want 0600", info.Mode().Perm())
	}
}
