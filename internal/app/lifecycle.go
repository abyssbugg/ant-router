package app

import "sync/atomic"

// State represents the lifecycle state as an integer constant.
type State int32

const (
	StateRunning  State = 0
	StateDraining State = 1
	StateStopped  State = 2
)

// Lifecycle manages the application lifecycle state transitions.
type Lifecycle struct {
	state atomic.Int32
}

// NewLifecycle creates a new Lifecycle in the Running state.
func NewLifecycle() *Lifecycle {
	lc := &Lifecycle{}
	lc.state.Store(int32(StateRunning))
	return lc
}

// State returns the current lifecycle state.
func (lc *Lifecycle) State() State {
	return State(lc.state.Load())
}

// BeginDrain attempts to transition from Running to Draining.
// It returns true if the transition succeeded, false otherwise.
// Only one concurrent Running→Draining transition can succeed.
func (lc *Lifecycle) BeginDrain() bool {
	return lc.state.CompareAndSwap(int32(StateRunning), int32(StateDraining))
}

// Stop transitions to Stopped state immediately and is idempotent.
func (lc *Lifecycle) Stop() {
	lc.state.Store(int32(StateStopped))
}
