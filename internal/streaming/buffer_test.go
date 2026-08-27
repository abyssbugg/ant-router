package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func bufferEvent(t *testing.T, sequence uint64, seed byte) canonical.Event {
	t.Helper()
	event, err := canonical.NewResponseStartedEvent(streamEventID(t, seed), streamResponseID(t, 1), sequence, "model")
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestPrecommitBufferPreservesOrderAndPeaks(t *testing.T) {
	options := DefaultPumpOptions()
	buffer, err := newPrecommitBuffer(options)
	if err != nil {
		t.Fatal(err)
	}
	first := bufferEvent(t, 0, 1)
	second := bufferEvent(t, 1, 2)
	if err := buffer.Append(first, false); err != nil {
		t.Fatal(err)
	}
	if err := buffer.Append(second, false); err != nil {
		t.Fatal(err)
	}
	if buffer.Count() != 2 || buffer.Bytes() == 0 || buffer.PeakEvents() != 2 || buffer.PeakBytes() != buffer.Bytes() {
		t.Fatalf("buffer metrics count=%d bytes=%d peakEvents=%d peakBytes=%d", buffer.Count(), buffer.Bytes(), buffer.PeakEvents(), buffer.PeakBytes())
	}
	drained := buffer.Drain()
	if len(drained) != 2 || drained[0].ID() != first.ID() || drained[1].ID() != second.ID() {
		t.Fatalf("drained=%#v", drained)
	}
	if buffer.Count() != 0 || buffer.Bytes() != 0 || buffer.PeakEvents() != 2 || buffer.PeakBytes() == 0 {
		t.Fatalf("post-drain metrics count=%d bytes=%d peakEvents=%d peakBytes=%d", buffer.Count(), buffer.Bytes(), buffer.PeakEvents(), buffer.PeakBytes())
	}
	drained[0] = canonical.Event{}
	if again := buffer.Drain(); len(again) != 0 {
		t.Fatalf("second drain=%#v", again)
	}
}

func TestPrecommitBufferEnforcesEventAndByteLimitsTransactionally(t *testing.T) {
	options := DefaultPumpOptions()
	options.MaxPrecommitEvents = 1
	buffer, _ := newPrecommitBuffer(options)
	if err := buffer.Append(bufferEvent(t, 0, 1), false); err != nil {
		t.Fatal(err)
	}
	beforeCount, beforeBytes := buffer.Count(), buffer.Bytes()
	if err := buffer.Append(bufferEvent(t, 1, 2), false); codeOfStream(err) != CodePrecommitLimitExceeded {
		t.Fatalf("event-limit code=%q error=%v", codeOfStream(err), err)
	}
	if buffer.Count() != beforeCount || buffer.Bytes() != beforeBytes {
		t.Error("event-limit failure mutated buffer")
	}

	options = DefaultPumpOptions()
	options.MaxPrecommitBytes = 1
	buffer, _ = newPrecommitBuffer(options)
	if err := buffer.Append(bufferEvent(t, 0, 3), false); codeOfStream(err) != CodePrecommitLimitExceeded {
		t.Fatalf("byte-limit code=%q error=%v", codeOfStream(err), err)
	}
	if buffer.Count() != 0 || buffer.Bytes() != 0 {
		t.Error("byte-limit failure mutated buffer")
	}
}

func TestPrecommitBufferAllowsOneSemanticCompletedImageBeyondOrdinaryBytes(t *testing.T) {
	options := DefaultPumpOptions()
	options.MaxPrecommitBytes = 1
	options.MaxPrecommitEvents = 2
	buffer, _ := newPrecommitBuffer(options)
	itemID := streamItemID(t, 1)
	image, _ := canonical.NewInlineImage([]byte{1, 2, 3}, "image/png", canonical.ImageDetailAuto, 0, 0)
	block, _ := canonical.NewImageBlock(image)
	event, _ := canonical.NewContentCompletedEvent(streamEventID(t, 1), streamResponseID(t, 1), 0, itemID, 0, block)
	if err := buffer.Append(event, true); err != nil {
		t.Fatalf("semantic image rejected: %v", err)
	}
	if err := buffer.Append(event, true); codeOfStream(err) != CodePrecommitLimitExceeded {
		t.Fatalf("second oversized image code=%q error=%v", codeOfStream(err), err)
	}
	if buffer.Count() != 1 {
		t.Fatalf("count=%d", buffer.Count())
	}
}

func TestPrecommitBufferRejectsInvalidEventAndOptions(t *testing.T) {
	if _, err := newPrecommitBuffer(PumpOptions{}); err == nil {
		t.Fatal("invalid options accepted")
	}
	buffer, _ := newPrecommitBuffer(DefaultPumpOptions())
	if err := buffer.Append(canonical.Event{}, false); err == nil {
		t.Fatal("invalid event accepted")
	}
}
