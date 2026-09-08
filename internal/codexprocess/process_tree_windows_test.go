//go:build windows

package codexprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsProcessCreationIsSuspended(t *testing.T) {
	if processCreationFlags&windows.CREATE_SUSPENDED == 0 {
		t.Fatal("Codex process is runnable before Job Object assignment")
	}
}

func TestRunnerCancellationTerminatesDescendantProcess(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	codexHome := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	runner := helperRunner(t)
	result := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, RunOptions{CodexHome: codexHome, Profile: "ant-router", Args: []string{"spawn-child", pidPath}})
		result <- err
	}()
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidPath)
		if err == nil {
			parsed, parseErr := strconv.Atoi(string(data))
			if parseErr == nil && parsed > 0 {
				pid = parsed
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		cancel()
		t.Fatal("descendant PID was not published")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not return after cancellation")
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(process)
	terminationDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(terminationDeadline) {
		status, err := windows.WaitForSingleObject(process, 0)
		if err == nil && status == windows.WAIT_OBJECT_0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("descendant process survived cancellation")
}
