package streaming

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func FuzzMachineEventStream(f *testing.F) {
	seed, err := os.ReadFile("testdata/reconstructed_response_stream.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > canonical.MaxRequestBytes {
			return
		}
		var raw []json.RawMessage
		if json.Unmarshal(data, &raw) != nil || len(raw) > canonical.MaxResponseItems*4 {
			return
		}
		machine, err := NewMachine(DefaultMachineOptions())
		if err != nil {
			t.Fatal(err)
		}
		for _, encoded := range raw {
			event, err := canonical.DecodeEvent(encoded)
			if err != nil {
				return
			}
			transition, err := machine.Accept(event)
			if err != nil {
				return
			}
			if response, ok := transition.Response(); ok && !response.Valid() {
				t.Fatal("invalid reconstructed response")
			}
		}
	})
}

func FuzzPumpFaultScript(f *testing.F) {
	events := loadFuzzGoldenEvents(f)
	f.Add(uint8(len(events)), int8(-1), uint8(0))
	f.Add(uint8(4), int8(0), uint8(1))
	f.Fuzz(func(t *testing.T, eventCount uint8, failPosition int8, mode uint8) {
		count := int(eventCount) % (len(events) + 1)
		source := &scriptedSource{events: events[:count]}
		if mode%3 == 1 {
			source.terminal = errSource
		}
		if mode%3 == 2 {
			source.terminal = io.EOF
		}
		sink := &recordingSink{failAt: -1}
		if failPosition >= 0 {
			sink.failAt = int(failPosition) % (len(events) + 1)
			sink.err = errSink
		}
		machine, err := NewMachine(DefaultMachineOptions())
		if err != nil {
			t.Fatal(err)
		}
		pump, err := NewPump(machine, source, sink, DefaultPumpOptions())
		if err != nil {
			t.Fatal(err)
		}
		result := pump.Run(context.Background())
		if !result.Valid() || result.DeliveredEvents() > result.ReceivedEvents() || source.closed.Load() != 1 {
			t.Fatalf("invalid result=%#v closed=%d", result, source.closed.Load())
		}
	})
}

func loadFuzzGoldenEvents(f *testing.F) []canonical.Event {
	f.Helper()
	raw, err := os.ReadFile("testdata/reconstructed_response_stream.json")
	if err != nil {
		f.Fatal(err)
	}
	var encoded []json.RawMessage
	if err := json.Unmarshal(raw, &encoded); err != nil {
		f.Fatal(err)
	}
	events := make([]canonical.Event, len(encoded))
	for index, value := range encoded {
		events[index], err = canonical.DecodeEvent(value)
		if err != nil {
			f.Fatal(err)
		}
	}
	return events
}
