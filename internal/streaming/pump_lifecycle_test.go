package streaming

import (
	"context"
	"strings"
	"testing"
)

func TestNewPumpRejectsInvalidDependencies(t *testing.T) {
	machine, _ := NewMachine(DefaultMachineOptions())
	validSource := &scriptedSource{}
	validSink := &recordingSink{failAt: -1}
	var typedSource *scriptedSource
	var typedSink *recordingSink
	cases := []struct {
		name    string
		machine *Machine
		source  EventSource
		sink    EventSink
		options PumpOptions
	}{
		{name: "nil machine", source: validSource, sink: validSink, options: DefaultPumpOptions()},
		{name: "nil source", machine: machine, sink: validSink, options: DefaultPumpOptions()},
		{name: "typed nil source", machine: machine, source: typedSource, sink: validSink, options: DefaultPumpOptions()},
		{name: "nil sink", machine: machine, source: validSource, options: DefaultPumpOptions()},
		{name: "typed nil sink", machine: machine, source: validSource, sink: typedSink, options: DefaultPumpOptions()},
		{name: "invalid options", machine: machine, source: validSource, sink: validSink},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPump(test.machine, test.source, test.sink, test.options); err == nil {
				t.Fatal("invalid pump accepted")
			}
		})
	}
}

func TestNewPumpRejectsAlreadyStartedMachine(t *testing.T) {
	machine, _ := NewMachine(DefaultMachineOptions())
	if _, err := machine.Accept(newResponseStartedForPump(t, streamResponseID(t, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPump(machine, &scriptedSource{}, &recordingSink{failAt: -1}, DefaultPumpOptions()); err == nil {
		t.Fatal("started machine accepted")
	}
}

func TestPumpRunIsSingleUseAndClosesSourceOnce(t *testing.T) {
	events := loadGoldenEvents(t)
	source := &scriptedSource{events: events}
	pump := newTestPump(t, source, &recordingSink{failAt: -1}, DefaultPumpOptions())
	first := pump.Run(context.Background())
	if first.TerminalKind() != TerminalCompleted {
		t.Fatalf("first=%#v", first)
	}
	second := pump.Run(context.Background())
	if resultCode(second) != CodeInvalidItemState || source.closed.Load() != 1 {
		t.Fatalf("second=%#v code=%q closed=%d", second, resultCode(second), source.closed.Load())
	}
}

func TestPumpJoinsCloseFailureWithoutReplacingPrimary(t *testing.T) {
	events := loadGoldenEvents(t)
	source := &scriptedSource{events: events[:1], terminal: errSource, closeError: errClose}
	result := newTestPump(t, source, &recordingSink{failAt: -1}, DefaultPumpOptions()).Run(context.Background())
	if resultCode(result) != CodeSourceFailed || !strings.Contains(result.Cause().Error(), string(CodeCloseFailed)) || source.closed.Load() != 1 {
		t.Fatalf("result=%#v code=%q cause=%v closed=%d", result, resultCode(result), result.Cause(), source.closed.Load())
	}
}
