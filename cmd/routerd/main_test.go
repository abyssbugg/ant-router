package main

import (
	"context"
	"errors"
	"io"
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
