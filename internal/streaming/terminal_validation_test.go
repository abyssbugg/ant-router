package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func completedTextItem(t *testing.T, b *streamBuilder, itemID canonical.ItemID, index int, value string) canonical.Item {
	t.Helper()
	start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: index, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	b.sequence++
	b.mustAccept(start)
	contentStart, _ := canonical.NewContentStartedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, canonical.ContentType("text"))
	b.sequence++
	b.mustAccept(contentStart)
	text, _ := canonical.NewTextContent(value)
	block, _ := canonical.NewTextBlock(text)
	contentDone, _ := canonical.NewContentCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, block)
	b.sequence++
	b.mustAccept(contentDone)
	message, _ := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
	item, _ := canonical.NewMessageItem(message)
	itemDone, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, index, item)
	b.sequence++
	b.mustAccept(itemDone)
	return item
}

func TestMachineRejectsMismatchedItemCompletionTransactionally(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 1)
	start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	b.sequence++
	b.mustAccept(start)
	text, _ := canonical.NewTextContent("wrong")
	block, _ := canonical.NewTextBlock(text)
	message, _ := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
	item, _ := canonical.NewMessageItem(message)
	completed, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, 0, item)
	before := b.machine.Snapshot()
	if _, err := b.machine.Accept(completed); codeOfStream(err) != CodeInvalidContentState {
		t.Fatalf("code=%q error=%v", codeOfStream(err), err)
	}
	if b.machine.Snapshot() != before {
		t.Error("mismatched item completion mutated machine")
	}
}

func TestMachineRejectsTerminalItemOrderAndUsageRegression(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	firstID := streamItemID(t, 1)
	secondID := streamItemID(t, 2)
	completedTextItem(t, b, firstID, 0, "one")
	completedTextItem(t, b, secondID, 1, "two")
	usage, _ := canonical.NewUsage(canonical.UsageParams{InputTokens: 5, OutputTokens: 5, TotalTokens: 10})
	usageEvent, _ := canonical.NewUsageUpdatedEvent(b.nextEventID(), b.responseID, b.sequence, usage)
	b.sequence++
	b.mustAccept(usageEvent)
	wrongOrder, _ := canonical.NewResponseCompletedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.FinishReason("stop"), usage, []canonical.ItemID{secondID, firstID})
	before := b.machine.Snapshot()
	if _, err := b.machine.Accept(wrongOrder); codeOfStream(err) != CodeTerminalMismatch {
		t.Fatalf("order code=%q error=%v", codeOfStream(err), err)
	}
	if b.machine.Snapshot() != before {
		t.Error("terminal item mismatch mutated machine")
	}
	regressed, _ := canonical.NewUsage(canonical.UsageParams{InputTokens: 4, OutputTokens: 5, TotalTokens: 9})
	badUsage, _ := canonical.NewResponseCompletedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.FinishReason("stop"), regressed, []canonical.ItemID{firstID, secondID})
	if _, err := b.machine.Accept(badUsage); codeOfStream(err) != CodeUsageRegression {
		t.Fatalf("usage code=%q error=%v", codeOfStream(err), err)
	}
	if b.machine.Snapshot() != before {
		t.Error("terminal usage regression mutated machine")
	}
}
