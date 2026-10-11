package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestMachineStructuralLimits(t *testing.T) {
	options := MachineOptions{MaxResponseBytes: 1024, MaxActiveItems: 1, MaxOpenContentPerItem: 1, MaxTrackedEvents: 1024}
	machine, _ := NewMachine(options)
	responseID := streamResponseID(t, 1)
	start, _ := canonical.NewResponseStartedEvent(streamEventID(t, 1), responseID, 0, "model")
	_, _ = machine.Accept(start)
	messageID := streamItemID(t, 1)
	message, _ := canonical.NewItemStartedEvent(streamEventID(t, 2), responseID, 1, canonical.ItemStartParams{Index: 0, ID: messageID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	if _, err := machine.Accept(message); err != nil {
		t.Fatalf("first item: %v", err)
	}
	second, _ := canonical.NewItemStartedEvent(streamEventID(t, 3), responseID, 2, canonical.ItemStartParams{Index: 1, ID: streamItemID(t, 2), Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	if _, err := machine.Accept(second); codeOfStream(err) != CodeResponseLimitExceeded {
		t.Errorf("item limit code=%q", codeOfStream(err))
	}
	content, _ := canonical.NewContentStartedEvent(streamEventID(t, 4), responseID, 2, messageID, 0, canonical.ContentType("text"))
	if _, err := machine.Accept(content); err != nil {
		t.Fatalf("first content: %v", err)
	}
	secondContent, _ := canonical.NewContentStartedEvent(streamEventID(t, 5), responseID, 3, messageID, 1, canonical.ContentType("text"))
	if _, err := machine.Accept(secondContent); codeOfStream(err) != CodeResponseLimitExceeded {
		t.Errorf("content limit code=%q", codeOfStream(err))
	}
}

func TestMachineRejectsEventsBeyondTrackedLimit(t *testing.T) {
	options := DefaultMachineOptions()
	options.MaxTrackedEvents = 2
	b := newStreamBuilder(t, options)
	messageID := streamItemID(t, 1)
	itemStart, err := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: messageID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	if err != nil {
		t.Fatal(err)
	}
	b.sequence++
	b.mustAccept(itemStart)
	before := b.machine.Snapshot()
	content, err := canonical.NewContentStartedEvent(b.nextEventID(), b.responseID, b.sequence, messageID, 0, canonical.ContentType("text"))
	if err != nil {
		t.Fatal(err)
	}
	b.sequence++
	if _, err := b.machine.Accept(content); codeOfStream(err) != CodeEventLimitExceeded {
		t.Fatalf("code=%q error=%v", codeOfStream(err), err)
	}
	if b.machine.Snapshot() != before {
		t.Error("tracked-event limit rejection mutated machine")
	}
}

func TestMachineOptionsRejectMissingTrackedLimit(t *testing.T) {
	options := DefaultMachineOptions()
	options.MaxTrackedEvents = 0
	if _, err := NewMachine(options); err == nil {
		t.Fatal("zero MaxTrackedEvents accepted")
	}
}
