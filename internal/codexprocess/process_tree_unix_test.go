//go:build !windows

package codexprocess

import (
	"context"
	"errors"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestRunnerCancellationTerminatesDescendantProcess(t *testing.T) {
	pidPath := t.TempDir() + "/child.pid"
	ctx, cancel := context.WithCancel(context.Background())
	runner := helperRunner(t)
	result := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, RunOptions{CodexHome: "/private/codex", Profile: "ant-router", Args: []string{"spawn-child", pidPath}})
		result <- err
	}()
	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidPath)
		if err == nil {
			pid, _ = strconv.Atoi(string(data))
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		cancel()
		t.Fatal("descendant PID was not published")
	}
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not return after cancellation")
	}
	terminationDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(terminationDeadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("descendant process survived cancellation")
}
