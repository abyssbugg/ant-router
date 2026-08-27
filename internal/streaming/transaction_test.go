package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestMachineCompletionMismatchIsTransactional(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 1)
	start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	b.sequence++
	b.mustAccept(start)
	content, _ := canonical.NewContentStartedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, canonical.ContentType("text"))
	b.sequence++
	b.mustAccept(content)
	delta, _ := canonical.NewTextDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, "hello")
	b.sequence++
	b.mustAccept(delta)
	wrongText, _ := canonical.NewTextContent("different")
	wrongBlock, _ := canonical.NewTextBlock(wrongText)
	completed, _ := canonical.NewContentCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, wrongBlock)
	before := b.machine.Snapshot()
	if _, err := b.machine.Accept(completed); codeOfStream(err) != CodeCompletionMismatch {
		t.Fatalf("code=%q error=%v", codeOfStream(err), err)
	}
	if b.machine.Snapshot() != before {
		t.Error("failed completion mutated machine")
	}
}

func TestMachineUsageRegressionTransactional(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	first, _ := canonical.NewUsage(canonical.UsageParams{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	firstEvent, _ := canonical.NewUsageUpdatedEvent(b.nextEventID(), b.responseID, b.sequence, first)
	b.sequence++
	b.mustAccept(firstEvent)
	regressed, _ := canonical.NewUsage(canonical.UsageParams{InputTokens: 9, OutputTokens: 5, TotalTokens: 14})
	regressedEvent, _ := canonical.NewUsageUpdatedEvent(b.nextEventID(), b.responseID, b.sequence, regressed)
	before := b.machine.Snapshot()
	if _, err := b.machine.Accept(regressedEvent); codeOfStream(err) != CodeUsageRegression {
		t.Fatalf("code=%q error=%v", codeOfStream(err), err)
	}
	if b.machine.Snapshot() != before {
		t.Error("usage regression mutated machine")
	}
}

func TestMachineResponseLimitTransactional(t *testing.T) {
	options := DefaultMachineOptions()
	options.MaxResponseBytes = 4
	b := newStreamBuilder(t, options)
	itemID := streamItemID(t, 1)
	start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	b.sequence++
	b.mustAccept(start)
	content, _ := canonical.NewContentStartedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, canonical.ContentType("text"))
	b.sequence++
	b.mustAccept(content)
	delta, _ := canonical.NewTextDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, "hello")
	before := b.machine.Snapshot()
	if _, err := b.machine.Accept(delta); codeOfStream(err) != CodeResponseLimitExceeded {
		t.Fatalf("code=%q error=%v", codeOfStream(err), err)
	}
	if b.machine.Snapshot() != before {
		t.Error("limit failure mutated machine")
	}
}
