package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

// State identifies the deterministic machine lifecycle.
type State string

const (
	// StateAwaitingResponse expects the first response_started event.
	StateAwaitingResponse State = "awaiting_response"
	// StatePrecommit has accepted structure but exposed no semantic output.
	StatePrecommit State = "precommit"
	// StateCommitted has crossed the semantic output commit point.
	StateCommitted State = "committed"
	// StateTerminal accepts no further events.
	StateTerminal State = "terminal"
)

// Valid reports whether the state is supported.
func (s State) Valid() bool {
	return s == StateAwaitingResponse || s == StatePrecommit || s == StateCommitted || s == StateTerminal
}

// DeliveryPhase identifies downstream visibility for pump results and errors.
type DeliveryPhase string

const (
	// DeliveryNone indicates no delivery phase applies yet.
	DeliveryNone DeliveryPhase = "none"
	// DeliveryPrecommit indicates no downstream event was attempted.
	DeliveryPrecommit DeliveryPhase = "precommit"
	// DeliveryPostcommit indicates downstream delivery was attempted.
	DeliveryPostcommit DeliveryPhase = "postcommit"
)

// Valid reports whether the delivery phase is supported.
func (p DeliveryPhase) Valid() bool {
	return p == DeliveryNone || p == DeliveryPrecommit || p == DeliveryPostcommit
}

// TerminalKind identifies the machine or pump terminal outcome.
type TerminalKind string

const (
	// TerminalNone indicates a nonterminal state.
	TerminalNone TerminalKind = "none"
	// TerminalCompleted indicates a completed canonical response.
	TerminalCompleted TerminalKind = "completed"
	// TerminalFailed indicates a canonical failure.
	TerminalFailed TerminalKind = "failed"
	// TerminalCancelled indicates cancellation.
	TerminalCancelled TerminalKind = "cancelled"
)

// Valid reports whether the terminal kind is supported.
func (k TerminalKind) Valid() bool {
	return k == TerminalNone || k == TerminalCompleted || k == TerminalFailed || k == TerminalCancelled
}

// DefaultMaxTrackedEvents bounds the duplicate-event-ID index so an
// adversarial stream of tiny deltas cannot grow machine memory without limit.
const DefaultMaxTrackedEvents = 1 << 18

// MachineOptions bounds deterministic stream reconstruction state.
type MachineOptions struct {
	MaxResponseBytes      int
	MaxActiveItems        int
	MaxOpenContentPerItem int
	MaxTrackedEvents      int
}

// DefaultMachineOptions returns limits derived from the canonical protocol.
func DefaultMachineOptions() MachineOptions {
	return MachineOptions{MaxResponseBytes: canonical.MaxRequestBytes, MaxActiveItems: canonical.MaxResponseItems, MaxOpenContentPerItem: canonical.MaxContentBlocks, MaxTrackedEvents: DefaultMaxTrackedEvents}
}

// Valid reports whether every machine limit is positive.
func (o MachineOptions) Valid() bool {
	return o.MaxResponseBytes > 0 && o.MaxActiveItems > 0 && o.MaxOpenContentPerItem > 0 && o.MaxTrackedEvents > 0
}
