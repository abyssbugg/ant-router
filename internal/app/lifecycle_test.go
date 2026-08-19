package app

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestNewLifecycle(t *testing.T) {
	lc := NewLifecycle()

	if lc.State() != StateRunning {
		t.Errorf("expected initial state to be StateRunning, got %v", lc.State())
	}
}

func TestBeginDrain(t *testing.T) {
	lc := NewLifecycle()

	// First call to BeginDrain should succeed and return true
	if !lc.BeginDrain() {
		t.Error("expected first BeginDrain() to return true")
	}

	if lc.State() != StateDraining {
		t.Errorf("expected state to be StateDraining after BeginDrain, got %v", lc.State())
	}

	// Second call to BeginDrain should return false
	if lc.BeginDrain() {
		t.Error("expected second BeginDrain() to return false")
	}

	if lc.State() != StateDraining {
		t.Errorf("expected state to remain StateDraining, got %v", lc.State())
	}
}

func TestStop(t *testing.T) {
	lc := NewLifecycle()

	lc.Stop()

	if lc.State() != StateStopped {
		t.Errorf("expected state to be StateStopped after Stop, got %v", lc.State())
	}
}

func TestBeginDrainConcurrency(t *testing.T) {
	lc := NewLifecycle()

	// Use a barrier to synchronize goroutine start
	var barrier sync.WaitGroup
	barrier.Add(1)

	// Track how many goroutines successfully called BeginDrain
	var successCount atomic.Int32
	successCount.Store(0)

	// Launch multiple simultaneous BeginDrain() calls
	numGoroutines := 5
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			// Wait for all goroutines to be ready
			barrier.Wait()
			// Attempt to begin drain
			if lc.BeginDrain() {
				successCount.Add(1)
			}
		}()
	}

	// Release the barrier to start all goroutines simultaneously
	barrier.Done()

	// Wait for all goroutines to complete
	wg.Wait()

	// Exactly one call should have returned true
	if successCount.Load() != 1 {
		t.Errorf("expected exactly 1 successful BeginDrain call, got %d", successCount.Load())
	}

	// Final state should be StateDraining
	if lc.State() != StateDraining {
		t.Errorf("expected final state to be StateDraining, got %v", lc.State())
	}
}

func TestBeginDrainAfterStop(t *testing.T) {
	lc := NewLifecycle()

	// Stop the lifecycle first
	lc.Stop()

	// BeginDrain should return false
	if lc.BeginDrain() {
		t.Error("expected BeginDrain() after Stop to return false")
	}

	// State should remain StateStopped
	if lc.State() != StateStopped {
		t.Errorf("expected state to remain StateStopped, got %v", lc.State())
	}
}

func TestStopIdempotent(t *testing.T) {
	lc := NewLifecycle()

	// First Stop
	lc.Stop()

	if lc.State() != StateStopped {
		t.Errorf("expected state to be StateStopped after first Stop, got %v", lc.State())
	}

	// Second Stop should be idempotent
	lc.Stop()

	if lc.State() != StateStopped {
		t.Errorf("expected state to remain StateStopped after second Stop, got %v", lc.State())
	}
}
