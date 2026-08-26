package streaming

import "testing"

func TestStreamingEnums(t *testing.T) {
	for _, value := range []State{StateAwaitingResponse, StatePrecommit, StateCommitted, StateTerminal} {
		if !value.Valid() {
			t.Errorf("state %q invalid", value)
		}
	}
	for _, value := range []DeliveryPhase{DeliveryNone, DeliveryPrecommit, DeliveryPostcommit} {
		if !value.Valid() {
			t.Errorf("phase %q invalid", value)
		}
	}
	for _, value := range []TerminalKind{TerminalNone, TerminalCompleted, TerminalFailed, TerminalCancelled} {
		if !value.Valid() {
			t.Errorf("terminal %q invalid", value)
		}
	}
	if State("bad").Valid() || DeliveryPhase("bad").Valid() || TerminalKind("bad").Valid() {
		t.Error("invalid enum accepted")
	}
}

func TestMachineOptions(t *testing.T) {
	options := DefaultMachineOptions()
	if options.MaxResponseBytes != 32<<20 || options.MaxActiveItems != 1024 || options.MaxOpenContentPerItem != 256 || !options.Valid() {
		t.Errorf("defaults=%#v", options)
	}
	for _, invalid := range []MachineOptions{{}, {MaxResponseBytes: -1, MaxActiveItems: 1, MaxOpenContentPerItem: 1}, {MaxResponseBytes: 1, MaxActiveItems: 0, MaxOpenContentPerItem: 1}, {MaxResponseBytes: 1, MaxActiveItems: 1, MaxOpenContentPerItem: 0}} {
		if invalid.Valid() {
			t.Errorf("invalid options accepted: %#v", invalid)
		}
	}
}
