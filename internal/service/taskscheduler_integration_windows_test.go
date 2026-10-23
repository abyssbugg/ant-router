//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// helperService is a stand-in for routerd: it accepts the "--log-file PATH"
// argument the task definition appends, writes a line to that file, and stays
// running until Task Scheduler stops it.
const helperService = `package main

import (
	"os"
	"time"
)

func main() {
	for i := 1; i+1 < len(os.Args); i++ {
		if os.Args[i] == "--log-file" {
			f, err := os.OpenFile(os.Args[i+1], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				os.Exit(1)
			}
			f.WriteString("helper started\n")
			f.Close()
		}
	}
	time.Sleep(10 * time.Minute)
}
`

// TestTaskSchedulerIntegration drives the real Windows Task Scheduler through
// the production backend and ExecRunner: install registers and starts the
// task, status reads the ScheduledTasks State enum, stop and uninstall are
// idempotent, and a task this package does not own is never touched. It
// mutates the current user's scheduled tasks, so it runs only when
// ANT_ROUTER_TASK_SCHEDULER_E2E=1 (set by the native Windows CI job).
func TestTaskSchedulerIntegration(t *testing.T) {
	if os.Getenv("ANT_ROUTER_TASK_SCHEDULER_E2E") != "1" {
		t.Skip("set ANT_ROUTER_TASK_SCHEDULER_E2E=1 to exercise the real Task Scheduler")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(helperService), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "helper-service.exe")
	build := exec.CommandContext(ctx, "go", "build", "-o", executable, src)
	build.Env = append(os.Environ(), "GOFLAGS=")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build helper service: %v\n%s", err, out)
	}

	label := fmt.Sprintf("ant-router-it-%d", time.Now().UnixNano())
	logPath := filepath.Join(dir, "service.log")
	spec := Spec{Label: label, Executable: executable, Args: []string{"--state-dir", filepath.Join(dir, "state")}, LogPath: logPath}
	backend := NewTaskSchedulerBackend(filepath.Join(dir, "backend-state"), &ExecRunner{})
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if err := backend.Uninstall(cleanupCtx, label); err != nil {
			t.Errorf("cleanup uninstall: %v", err)
		}
	})

	if status, err := backend.Status(ctx, label); err != nil || status.State != StateNotInstalled {
		t.Fatalf("status before install = %+v, %v; want not installed", status, err)
	}
	if err := backend.Install(ctx, spec); err != nil {
		t.Fatalf("install: %v", err)
	}
	waitForState(ctx, t, backend, label, StateRunning)
	waitForLog(ctx, t, logPath, "helper started")

	// Reinstalling an owned task replaces it in place.
	if err := backend.Install(ctx, spec); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	waitForState(ctx, t, backend, label, StateRunning)

	if err := backend.Stop(ctx, label); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitForState(ctx, t, backend, label, StateStopped)
	if err := backend.Stop(ctx, label); err != nil {
		t.Fatalf("stop of a stopped task must be idempotent: %v", err)
	}
	if err := backend.Start(ctx, label); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForState(ctx, t, backend, label, StateRunning)

	if err := backend.Uninstall(ctx, label); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if status, err := backend.Status(ctx, label); err != nil || status.State != StateNotInstalled {
		t.Fatalf("status after uninstall = %+v, %v; want not installed", status, err)
	}
	if err := backend.Uninstall(ctx, label); err != nil {
		t.Fatalf("uninstall of an absent task must be idempotent: %v", err)
	}
	var notInstalled ErrNotInstalled
	if err := backend.Start(ctx, label); !errors.As(err, &notInstalled) {
		t.Fatalf("start after uninstall = %v, want ErrNotInstalled", err)
	}

	// A task registered under the same name by someone else is never
	// started, stopped, replaced, or deleted.
	foreign := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$action = New-ScheduledTaskAction -Execute %s
Register-ScheduledTask -TaskPath '\AntRouter\' -TaskName %s -Action $action -Description 'not ours' | Out-Null`,
		psQuote(executable), psQuote(label))
	if _, err := backend.script(ctx, "register foreign task", foreign); err != nil {
		t.Fatalf("register foreign task: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		script := fmt.Sprintf("$ErrorActionPreference = 'Stop'\nUnregister-ScheduledTask -TaskPath '\\AntRouter\\' -TaskName %s -Confirm:$false", psQuote(label))
		if _, err := backend.script(cleanupCtx, "remove foreign task", script); err != nil {
			t.Errorf("remove foreign task: %v", err)
		}
	})
	var notOwned ErrNotOwned
	if err := backend.Install(ctx, spec); !errors.As(err, &notOwned) {
		t.Fatalf("install over foreign task = %v, want ErrNotOwned", err)
	}
	if err := backend.Uninstall(ctx, label); !errors.As(err, &notOwned) {
		t.Fatalf("uninstall of foreign task = %v, want ErrNotOwned", err)
	}
	if _, err := backend.Status(ctx, label); !errors.As(err, &notOwned) {
		t.Fatalf("status of foreign task = %v, want ErrNotOwned", err)
	}
}

func waitForState(ctx context.Context, t *testing.T, backend *TaskSchedulerBackend, label string, want State) {
	t.Helper()
	var last Status
	var lastErr error
	for ctx.Err() == nil {
		last, lastErr = backend.Status(ctx, label)
		if lastErr == nil && last.State == want {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("task %s never reached %v; last status %+v, err %v", label, want, last, lastErr)
}

func waitForLog(ctx context.Context, t *testing.T, path, want string) {
	t.Helper()
	for ctx.Err() == nil {
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), want) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	data, err := os.ReadFile(path)
	t.Fatalf("log %s never contained %q; content %q, err %v", path, want, data, err)
}
