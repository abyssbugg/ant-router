package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestMachineReconstructsTextResponseAndCommitsOnFirstDelta(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 1)
	itemStart, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	b.sequence++
	b.mustAccept(itemStart)
	contentStart, _ := canonical.NewContentStartedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, canonical.ContentType("text"))
	b.sequence++
	b.mustAccept(contentStart)
	firstDelta, _ := canonical.NewTextDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, "hel")
	b.sequence++
	transition := b.mustAccept(firstDelta)
	if !transition.SemanticOutput() || !transition.JustCommitted() || b.machine.State() != StateCommitted {
		t.Errorf("first delta transition=%#v state=%q", transition, b.machine.State())
	}
	secondDelta, _ := canonical.NewTextDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, "lo")
	b.sequence++
	transition = b.mustAccept(secondDelta)
	if !transition.SemanticOutput() || transition.JustCommitted() {
		t.Errorf("second delta transition=%#v", transition)
	}
	text, _ := canonical.NewTextContent("hello")
	block, _ := canonical.NewTextBlock(text)
	contentDone, _ := canonical.NewContentCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, block)
	b.sequence++
	b.mustAccept(contentDone)
	message, _ := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
	item, _ := canonical.NewMessageItem(message)
	itemDone, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, 0, item)
	b.sequence++
	b.mustAccept(itemDone)
	usage, _ := canonical.NewUsage(canonical.UsageParams{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	usageEvent, _ := canonical.NewUsageUpdatedEvent(b.nextEventID(), b.responseID, b.sequence, usage)
	b.sequence++
	b.mustAccept(usageEvent)
	completed, _ := canonical.NewResponseCompletedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.FinishReason("stop"), usage, []canonical.ItemID{itemID})
	b.sequence++
	transition = b.mustAccept(completed)
	response, ok := transition.Response()
	if !ok || transition.TerminalKind() != TerminalCompleted || !response.Valid() || response.Model() != "model" || len(response.Items()) != 1 || response.Usage().TotalTokens() != 15 || b.machine.State() != StateTerminal {
		t.Fatalf("terminal transition=%#v response=%#v", transition, response)
	}
	if snapshot := b.machine.Snapshot(); snapshot.CompletedItems() != 1 || snapshot.OpenContent() != 0 || snapshot.TerminalKind() != TerminalCompleted || snapshot.ReconstructedBytes() != 5 {
		t.Errorf("snapshot=%#v", snapshot)
	}
}

func TestMachineAtomicImageCompletionCommitsWithoutDelta(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 1)
	itemStart, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	b.sequence++
	b.mustAccept(itemStart)
	contentStart, _ := canonical.NewContentStartedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, canonical.ContentType("image"))
	b.sequence++
	b.mustAccept(contentStart)
	image, _ := canonical.NewInlineImage([]byte{1, 2, 3}, "image/png", canonical.ImageDetailAuto, 0, 0)
	block, _ := canonical.NewImageBlock(image)
	contentDone, _ := canonical.NewContentCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, block)
	transition := b.mustAccept(contentDone)
	if !transition.SemanticOutput() || !transition.JustCommitted() || b.machine.Snapshot().ReconstructedBytes() != 3 {
		t.Errorf("image transition=%#v snapshot=%#v", transition, b.machine.Snapshot())
	}
}
