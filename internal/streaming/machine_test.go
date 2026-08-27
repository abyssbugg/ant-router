package streaming

import (
	"errors"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestMachineResponseStart(t *testing.T) {
	machine, err := NewMachine(DefaultMachineOptions())
	if err != nil {
		t.Fatalf("NewMachine: %v", err)
	}
	if machine.State() != StateAwaitingResponse {
		t.Errorf("initial state=%q", machine.State())
	}
	event := simpleEvent(t)
	transition, err := machine.Accept(event)
	if err != nil || transition.Event().ID() != event.ID() || transition.SemanticOutput() || transition.JustCommitted() {
		t.Fatalf("Accept()=%#v,%v", transition, err)
	}
	snapshot := machine.Snapshot()
	if snapshot.State() != StatePrecommit || snapshot.ResponseID() != event.ResponseID() || snapshot.NextSequence() != 1 || snapshot.StartedItems() != 0 || snapshot.Committed() {
		t.Errorf("snapshot=%#v", snapshot)
	}
}

func TestMachineRejectsInvalidFirstEventTransactionally(t *testing.T) {
	machine, _ := NewMachine(DefaultMachineOptions())
	before := machine.Snapshot()
	itemEvent, _ := canonical.NewItemStartedEvent(streamEventID(t, 2), streamResponseID(t, 1), 0, canonical.ItemStartParams{Index: 0, ID: streamItemID(t, 1), Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	_, err := machine.Accept(itemEvent)
	if codeOfStream(err) != CodeInvalidSequence {
		t.Fatalf("code=%q error=%v", codeOfStream(err), err)
	}
	var streamErr *Error
	if !errors.As(err, &streamErr) || streamErr.Phase != DeliveryNone {
		t.Errorf("phase=%v", streamErr)
	}
	if machine.Snapshot() != before {
		t.Error("rejected first event mutated machine")
	}
}

func TestNewMachineRejectsInvalidOptions(t *testing.T) {
	if _, err := NewMachine(MachineOptions{}); err == nil {
		t.Error("invalid options accepted")
	}
}

func codeOfStream(err error) ErrorCode { code, _ := CodeOf(err); return code }
