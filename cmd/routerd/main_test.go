package main

import (
	"context"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/app"
)

func TestRunWithCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Context is already cancelled

	lifecycle := app.NewLifecycle()

	code := run(ctx, lifecycle)

	state := lifecycle.State()

	if code != 0 {
		t.Errorf("run with cancelled context: expected exit code 0, got %d", code)
	}

	if state != app.StateStopped {
		t.Errorf("run with cancelled context: expected lifecycle state %v, got %v", app.StateStopped, state)
	}
}

func TestRunWithActiveCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	lifecycle := app.NewLifecycle()

	result := make(chan int, 1)
	go func() {
		result <- run(ctx, lifecycle)
	}()

	cancel()

	select {
	case code := <-result:
		state := lifecycle.State()

		if code != 0 {
			t.Errorf("active cancellation: expected exit code 0, got %d", code)
		}

		if state != app.StateStopped {
			t.Errorf("active cancellation: expected final state %v, got %v", app.StateStopped, state)
		}
	case <-time.After(5 * time.Second):
		t.Error("active cancellation: run() did not return within timeout (possible hang)")
	}
}
