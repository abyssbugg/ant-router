package streaming

import (
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func startCustomItem(t *testing.T, b *streamBuilder, itemID canonical.ItemID, callID canonical.ToolCallID) {
	t.Helper()
	start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: len(b.machine.itemOrder), ID: itemID, Type: canonical.ItemType("custom_tool_call"), CallID: callID, Namespace: "workspace", Name: "apply_patch"})
	b.sequence++
	b.mustAccept(start)
}

func TestMachineReconstructsCustomInputAndCommitsOnFirstDelta(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID, callID := streamItemID(t, 30), streamCallID(t, 30)
	startCustomItem(t, b, itemID, callID)
	for index, fragment := range []string{"*** Begin", " Patch"} {
		event, _ := canonical.NewCustomInputDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, fragment)
		b.sequence++
		transition := b.mustAccept(event)
		if !transition.SemanticOutput() || transition.JustCommitted() != (index == 0) {
			t.Fatalf("delta %d transition = %#v", index, transition)
		}
	}
	input := "*** Begin Patch"
	inputDone, _ := canonical.NewCustomInputCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, input)
	b.sequence++
	if transition := b.mustAccept(inputDone); transition.SemanticOutput() || transition.JustCommitted() {
		t.Fatalf("streamed input completion = %#v", transition)
	}
	call, _ := canonical.NewNamespacedCustomToolCall(itemID, callID, "workspace", "apply_patch", input)
	item, _ := canonical.NewCustomToolCallItem(call)
	itemDone, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, 0, item)
	b.sequence++
	b.mustAccept(itemDone)
	usage := canonical.Usage{}
	terminal, _ := canonical.NewResponseCompletedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.FinishReason("tool_call"), usage, []canonical.ItemID{itemID})
	transition := b.mustAccept(terminal)
	response, ok := transition.Response()
	if !ok || !response.Valid() || response.Items()[0].Type() != canonical.ItemType("custom_tool_call") || response.Items()[0].ID() != itemID {
		t.Fatalf("terminal custom response = %#v, %v", response, ok)
	}
	if got := b.machine.Snapshot().ReconstructedBytes(); got != len(input) {
		t.Fatalf("reconstructed bytes = %d, want %d", got, len(input))
	}
}

func TestMachineAtomicCustomInputCommitsAtCompletion(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID, callID := streamItemID(t, 31), streamCallID(t, 31)
	startCustomItem(t, b, itemID, callID)
	inputDone, _ := canonical.NewCustomInputCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, "patch")
	b.sequence++
	transition := b.mustAccept(inputDone)
	if !transition.SemanticOutput() || !transition.JustCommitted() || b.machine.Snapshot().ReconstructedBytes() != 5 {
		t.Fatalf("atomic custom completion = %#v snapshot = %#v", transition, b.machine.Snapshot())
	}
}

func TestMachineCustomInputFailuresAreTransactional(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID, callID := streamItemID(t, 32), streamCallID(t, 32)
	startCustomItem(t, b, itemID, callID)
	call, _ := canonical.NewNamespacedCustomToolCall(itemID, callID, "workspace", "apply_patch", "patch")
	item, _ := canonical.NewCustomToolCallItem(call)
	itemDone, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, 0, item)
	before := b.machine.Snapshot()
	if _, err := b.machine.Accept(itemDone); codeOfStream(err) != CodeInvalidItemState || b.machine.Snapshot() != before {
		t.Fatalf("early item completion code=%q snapshot=%#v", codeOfStream(err), b.machine.Snapshot())
	}
	delta, _ := canonical.NewCustomInputDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, "patch")
	b.sequence++
	b.mustAccept(delta)
	wrong, _ := canonical.NewCustomInputCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, "different")
	before = b.machine.Snapshot()
	if _, err := b.machine.Accept(wrong); codeOfStream(err) != CodeCompletionMismatch || b.machine.Snapshot() != before {
		t.Fatalf("mismatched input code=%q snapshot=%#v", codeOfStream(err), b.machine.Snapshot())
	}
}

func TestMachineCustomInputRespectsResponseLimit(t *testing.T) {
	const input = "patch"

	t.Run("exact limit", func(t *testing.T) {
		options := DefaultMachineOptions()
		options.MaxResponseBytes = len(input)
		b := newStreamBuilder(t, options)
		itemID, callID := streamItemID(t, 33), streamCallID(t, 33)
		startCustomItem(t, b, itemID, callID)
		event, _ := canonical.NewCustomInputCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, input)
		if _, err := b.machine.Accept(event); err != nil || b.machine.Snapshot().ReconstructedBytes() != len(input) {
			t.Fatalf("exact limit err=%v snapshot=%#v", err, b.machine.Snapshot())
		}
	})

	t.Run("limit exceeded is transactional", func(t *testing.T) {
		options := DefaultMachineOptions()
		options.MaxResponseBytes = len(input) - 1
		b := newStreamBuilder(t, options)
		itemID, callID := streamItemID(t, 34), streamCallID(t, 34)
		startCustomItem(t, b, itemID, callID)
		event, _ := canonical.NewCustomInputCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, strings.Repeat("x", len(input)))
		before := b.machine.Snapshot()
		if _, err := b.machine.Accept(event); codeOfStream(err) != CodeResponseLimitExceeded || b.machine.Snapshot() != before {
			t.Fatalf("limit code=%q snapshot=%#v", codeOfStream(err), b.machine.Snapshot())
		}
	})

	t.Run("streamed deltas reach exact limit", func(t *testing.T) {
		options := DefaultMachineOptions()
		options.MaxResponseBytes = len(input)
		b := newStreamBuilder(t, options)
		itemID, callID := streamItemID(t, 35), streamCallID(t, 35)
		startCustomItem(t, b, itemID, callID)
		event, _ := canonical.NewCustomInputDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, input)
		b.sequence++
		transition := b.mustAccept(event)
		if !transition.SemanticOutput() || !transition.JustCommitted() || b.machine.Snapshot().ReconstructedBytes() != len(input) {
			t.Fatalf("streamed delta transition = %#v snapshot = %#v", transition, b.machine.Snapshot())
		}
		completion, _ := canonical.NewCustomInputCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, input)
		b.sequence++
		if transition := b.mustAccept(completion); transition.SemanticOutput() || transition.JustCommitted() {
			t.Fatalf("completion after streamed delta = %#v", transition)
		}
	})

	t.Run("streamed delta beyond response limit is transactional", func(t *testing.T) {
		options := DefaultMachineOptions()
		options.MaxResponseBytes = len(input) - 1
		b := newStreamBuilder(t, options)
		itemID, callID := streamItemID(t, 36), streamCallID(t, 36)
		startCustomItem(t, b, itemID, callID)
		event, _ := canonical.NewCustomInputDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, input)
		before := b.machine.Snapshot()
		if _, err := b.machine.Accept(event); codeOfStream(err) != CodeResponseLimitExceeded || b.machine.Snapshot() != before {
			t.Fatalf("limit code=%q snapshot=%#v", codeOfStream(err), b.machine.Snapshot())
		}
		completion, _ := canonical.NewCustomInputCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, "")
		b.sequence++
		transition := b.mustAccept(completion)
		if !transition.SemanticOutput() || !transition.JustCommitted() || b.machine.Snapshot().ReconstructedBytes() != 0 {
			t.Fatalf("completion after rejected delta = %#v snapshot = %#v", transition, b.machine.Snapshot())
		}
	})

	t.Run("rejected delta preserves accumulated buffer", func(t *testing.T) {
		options := DefaultMachineOptions()
		options.MaxResponseBytes = 4
		b := newStreamBuilder(t, options)
		itemID, callID := streamItemID(t, 37), streamCallID(t, 37)
		startCustomItem(t, b, itemID, callID)
		first, _ := canonical.NewCustomInputDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, "ab")
		b.sequence++
		b.mustAccept(first)
		oversized, _ := canonical.NewCustomInputDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, "cde")
		before := b.machine.Snapshot()
		if _, err := b.machine.Accept(oversized); codeOfStream(err) != CodeResponseLimitExceeded || b.machine.Snapshot() != before {
			t.Fatalf("oversized delta code=%q snapshot=%#v", codeOfStream(err), b.machine.Snapshot())
		}
		continuation, _ := canonical.NewCustomInputDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, "cd")
		b.sequence++
		transition := b.mustAccept(continuation)
		if !transition.SemanticOutput() || b.machine.Snapshot().ReconstructedBytes() != 4 {
			t.Fatalf("continuation delta = %#v snapshot = %#v", transition, b.machine.Snapshot())
		}
		completion, _ := canonical.NewCustomInputCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, "abcd")
		b.sequence++
		if transition := b.mustAccept(completion); transition.SemanticOutput() || transition.JustCommitted() {
			t.Fatalf("completion = %#v", transition)
		}
	})

	t.Run("accumulated deltas beyond custom input cap are transactional", func(t *testing.T) {
		b := newStreamBuilder(t, DefaultMachineOptions())
		itemID, callID := streamItemID(t, 38), streamCallID(t, 38)
		startCustomItem(t, b, itemID, callID)
		filler := strings.Repeat("x", canonical.MaxCustomToolInputBytes-256)
		first, err := canonical.NewCustomInputDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, filler)
		if err != nil {
			t.Fatalf("first delta constructor: %v", err)
		}
		b.sequence++
		b.mustAccept(first)
		second, err := canonical.NewCustomInputDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, filler)
		if err != nil {
			t.Fatalf("second delta constructor: %v", err)
		}
		before := b.machine.Snapshot()
		if _, err := b.machine.Accept(second); codeOfStream(err) != CodeResponseLimitExceeded || b.machine.Snapshot() != before {
			t.Fatalf("cap code=%q snapshot=%#v", codeOfStream(err), b.machine.Snapshot())
		}
	})
}
