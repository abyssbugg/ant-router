package streaming

import (
	"bytes"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

type streamBuilder struct {
	t          *testing.T
	machine    *Machine
	responseID canonical.ResponseID
	sequence   uint64
	eventSeed  byte
}

func newStreamBuilder(t *testing.T, options MachineOptions) *streamBuilder {
	t.Helper()
	machine, err := NewMachine(options)
	if err != nil {
		t.Fatalf("NewMachine: %v", err)
	}
	builder := &streamBuilder{t: t, machine: machine, responseID: streamResponseID(t, 1), eventSeed: 1}
	start, err := canonical.NewResponseStartedEvent(builder.nextEventID(), builder.responseID, 0, "model")
	if err != nil {
		t.Fatalf("NewResponseStartedEvent: %v", err)
	}
	builder.mustAccept(start)
	builder.sequence = 1
	return builder
}

func (b *streamBuilder) nextEventID() canonical.EventID {
	b.eventSeed++
	id, err := canonical.GenerateEventID(bytes.NewReader(bytes.Repeat([]byte{b.eventSeed}, canonical.IDRandomBytes)))
	if err != nil {
		b.t.Fatalf("GenerateEventID: %v", err)
	}
	return id
}

func (b *streamBuilder) mustAccept(event canonical.Event) Transition {
	b.t.Helper()
	transition, err := b.machine.Accept(event)
	if err != nil {
		b.t.Fatalf("Accept(%q): %v", event.Type(), err)
	}
	return transition
}
