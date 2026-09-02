package streaming

import (
	"errors"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// Machine deterministically validates and reconstructs one canonical stream.
// Machine is not safe for concurrent use; call Accept serially from one goroutine.
type Machine struct {
	options            MachineOptions
	state              State
	responseID         canonical.ResponseID
	model              string
	nextSequence       uint64
	eventIDs           map[canonical.EventID]struct{}
	items              map[canonical.ItemID]*itemState
	itemOrder          []canonical.ItemID
	latestUsage        canonical.Usage
	continuation       *canonical.Continuation
	continuationBytes  int
	committed          bool
	terminal           TerminalKind
	reconstructedBytes int
}

// NewMachine constructs an empty machine with validated limits.
func NewMachine(options MachineOptions) (*Machine, error) {
	if !options.Valid() {
		return nil, &Error{Code: CodeInvalidItemState, Path: "machine.options", Phase: DeliveryNone, Err: errors.New("machine options are invalid")}
	}
	return &Machine{options: options, state: StateAwaitingResponse, eventIDs: make(map[canonical.EventID]struct{}), items: make(map[canonical.ItemID]*itemState)}, nil
}

// State returns the current machine lifecycle state.
func (m *Machine) State() State { return m.state }

// Snapshot returns safe structural machine metadata.
func (m *Machine) Snapshot() Snapshot {
	completed, open := 0, 0
	for _, item := range m.items {
		if item.completed {
			completed++
		}
		for _, content := range item.contents {
			if !content.completed {
				open++
			}
		}
	}
	return newSnapshot(m.state, m.responseID, m.nextSequence, len(m.itemOrder), completed, open, m.latestUsage, m.continuation != nil, m.continuationBytes, m.committed, m.terminal, m.reconstructedBytes)
}

// Accept validates and atomically applies one canonical event.
func (m *Machine) Accept(event canonical.Event) (Transition, error) {
	if m.state == StateTerminal {
		return Transition{}, m.error(CodeEventAfterTerminal, "event")
	}
	if !event.Valid() {
		return Transition{}, m.error(CodeInvalidItemState, "event")
	}
	if m.state == StateAwaitingResponse {
		return m.acceptInitial(event)
	}
	if _, duplicate := m.eventIDs[event.ID()]; duplicate {
		return Transition{}, m.error(CodeDuplicateEvent, "event.id")
	}
	if event.Sequence() != m.nextSequence {
		return Transition{}, m.error(CodeInvalidSequence, "event.sequence")
	}
	if event.ResponseID() != m.responseID {
		return Transition{}, m.error(CodeResponseMismatch, "event.response_id")
	}
	outcome := acceptOutcome{terminal: TerminalNone}
	var err error
	switch event.Type() {
	case canonical.EventType("continuation_updated"):
		err = m.acceptContinuationUpdated(event)
	case canonical.EventType("item_started"):
		err = m.acceptItemStarted(event)
	case canonical.EventType("content_started"):
		err = m.acceptContentStarted(event)
	case canonical.EventType("text_delta"), canonical.EventType("json_delta"), canonical.EventType("tool_arguments_delta"):
		outcome, err = m.acceptDelta(event)
	case canonical.EventType("content_completed"):
		outcome, err = m.acceptContentCompleted(event)
	case canonical.EventType("item_completed"):
		outcome, err = m.acceptItemCompleted(event)
	case canonical.EventType("usage_updated"):
		outcome, err = m.acceptUsage(event)
	case canonical.EventType("response_completed"), canonical.EventType("response_failed"), canonical.EventType("response_cancelled"):
		outcome, err = m.acceptTerminal(event)
	default:
		err = m.error(CodeInvalidItemState, "event.type")
	}
	if err != nil {
		return Transition{}, err
	}
	if outcome.terminal == "" {
		outcome.terminal = TerminalNone
	}
	m.eventIDs[event.ID()] = struct{}{}
	m.nextSequence++
	justCommitted := false
	if outcome.semantic && !m.committed {
		m.committed = true
		justCommitted = true
	}
	if outcome.usage != nil {
		m.latestUsage = *outcome.usage
	}
	if outcome.terminal != TerminalNone {
		m.terminal = outcome.terminal
		m.state = StateTerminal
	} else if m.committed {
		m.state = StateCommitted
	} else {
		m.state = StatePrecommit
	}
	return newTransition(event, outcome.semantic, justCommitted, outcome.terminal, outcome.response, outcome.failure, outcome.usage), nil
}

func (m *Machine) acceptInitial(event canonical.Event) (Transition, error) {
	if event.Sequence() != 0 || event.Type() != canonical.EventType("response_started") {
		return Transition{}, m.error(CodeInvalidSequence, "event.sequence")
	}
	payload, ok := event.ResponseStarted()
	if !ok || !payload.Valid() {
		return Transition{}, m.error(CodeInvalidItemState, "event.response_started")
	}
	m.responseID = event.ResponseID()
	m.model = payload.Model()
	m.eventIDs[event.ID()] = struct{}{}
	m.nextSequence = 1
	m.state = StatePrecommit
	return newTransition(event, false, false, TerminalNone, nil, nil, nil), nil
}

func (m *Machine) phase() DeliveryPhase {
	if m.state == StateAwaitingResponse {
		return DeliveryNone
	}
	if m.committed {
		return DeliveryPostcommit
	}
	return DeliveryPrecommit
}

func (m *Machine) error(code ErrorCode, path string) error {
	return &Error{Code: code, Path: path, Phase: m.phase(), Err: errors.New("stream transition is invalid")}
}
