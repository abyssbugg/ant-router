package streaming

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestMachineReconstructsGoldenStream(t *testing.T) {
	raw, err := os.ReadFile("testdata/reconstructed_response_stream.json")
	if err != nil {
		t.Fatal(err)
	}
	var encoded []json.RawMessage
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatal(err)
	}
	machine, err := NewMachine(DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	var terminal Transition
	for index, value := range encoded {
		event, err := canonical.DecodeEvent(value)
		if err != nil {
			t.Fatalf("event %d decode: %v", index, err)
		}
		terminal, err = machine.Accept(event)
		if err != nil {
			t.Fatalf("event %d accept: %v", index, err)
		}
	}
	response, ok := terminal.Response()
	if !ok || terminal.TerminalKind() != TerminalCompleted {
		t.Fatalf("terminal=%#v", terminal)
	}
	got, err := canonical.EncodeResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/reconstructed_response.json")
	if err != nil {
		t.Fatal(err)
	}
	want = bytes.TrimSpace(want)
	if !bytes.Equal(got, want) {
		t.Fatalf("response mismatch\ngot:  %s\nwant: %s", got, want)
	}
	if snapshot := machine.Snapshot(); snapshot.NextSequence() != uint64(len(encoded)) || snapshot.CompletedItems() != 2 || snapshot.TerminalKind() != TerminalCompleted {
		t.Errorf("snapshot=%#v", snapshot)
	}
}
