package streaming

import (
	"context"
	"testing"
)

func TestPumpBufferLimitLeavesMachineBeforeRejectedEvent(t *testing.T) {
	events := loadGoldenEvents(t)
	machine, err := NewMachine(DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	options := DefaultPumpOptions()
	options.MaxPrecommitEvents = 2
	source := &scriptedSource{events: events[:3]}
	sink := &recordingSink{failAt: -1}
	pump, err := NewPump(machine, source, sink, options)
	if err != nil {
		t.Fatal(err)
	}
	result := pump.Run(context.Background())
	if resultCode(result) != CodePrecommitLimitExceeded || result.Phase() != DeliveryPrecommit {
		t.Fatalf("result=%#v code=%q", result, resultCode(result))
	}
	beforeRetry := machine.Snapshot()
	if beforeRetry.NextSequence() != 2 || beforeRetry.OpenContent() != 0 || beforeRetry.Committed() {
		t.Fatalf("machine advanced past rejected event: %#v", beforeRetry)
	}
	if _, err := machine.Accept(events[2]); err != nil {
		t.Fatalf("rejected event cannot be accepted directly: %v", err)
	}
	if machine.Snapshot().NextSequence() != 3 {
		t.Fatalf("machine did not advance after direct retry: %#v", machine.Snapshot())
	}
}
