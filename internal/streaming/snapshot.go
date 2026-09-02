package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

// Snapshot contains safe structural machine diagnostics without stream content.
type Snapshot struct {
	state              State
	responseID         canonical.ResponseID
	nextSequence       uint64
	startedItems       int
	completedItems     int
	openContent        int
	latestUsage        canonical.Usage
	hasContinuation    bool
	continuationBytes  int
	committed          bool
	terminal           TerminalKind
	reconstructedBytes int
}

func newSnapshot(state State, responseID canonical.ResponseID, next uint64, started, completed, open int, usage canonical.Usage, hasContinuation bool, continuationBytes int, committed bool, terminal TerminalKind, byteCount int) Snapshot {
	return Snapshot{state: state, responseID: responseID, nextSequence: next, startedItems: started, completedItems: completed, openContent: open, latestUsage: usage, hasContinuation: hasContinuation, continuationBytes: continuationBytes, committed: committed, terminal: terminal, reconstructedBytes: byteCount}
}

// State returns the machine lifecycle state.
func (s Snapshot) State() State { return s.state }

// ResponseID returns the established response ID.
func (s Snapshot) ResponseID() canonical.ResponseID { return s.responseID }

// NextSequence returns the exact next expected sequence.
func (s Snapshot) NextSequence() uint64 { return s.nextSequence }

// StartedItems returns the number of started items.
func (s Snapshot) StartedItems() int { return s.startedItems }

// CompletedItems returns the number of completed items.
func (s Snapshot) CompletedItems() int { return s.completedItems }

// OpenContent returns the number of open content blocks.
func (s Snapshot) OpenContent() int { return s.openContent }

// LatestUsage returns the latest accepted usage counters.
func (s Snapshot) LatestUsage() canonical.Usage { return s.latestUsage }

// HasContinuation reports whether the machine has accepted opaque continuation.
func (s Snapshot) HasContinuation() bool { return s.hasContinuation }

// ContinuationBytes returns the safe opaque continuation byte count.
func (s Snapshot) ContinuationBytes() int { return s.continuationBytes }

// Committed reports whether semantic output has committed.
func (s Snapshot) Committed() bool { return s.committed }

// TerminalKind returns the terminal outcome.
func (s Snapshot) TerminalKind() TerminalKind { return s.terminal }

// ReconstructedBytes returns the safe accumulated byte count.
func (s Snapshot) ReconstructedBytes() int { return s.reconstructedBytes }
