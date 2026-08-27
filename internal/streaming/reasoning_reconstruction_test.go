package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestMachineReconstructsReasoningSummary(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 1)
	start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("reasoning_summary")})
	b.sequence++
	b.mustAccept(start)
	content, _ := canonical.NewContentStartedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, canonical.ContentType("text"))
	b.sequence++
	b.mustAccept(content)
	text, _ := canonical.NewTextContent("summary")
	block, _ := canonical.NewTextBlock(text)
	completed, _ := canonical.NewContentCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, block)
	b.sequence++
	transition := b.mustAccept(completed)
	if !transition.SemanticOutput() || !transition.JustCommitted() {
		t.Errorf("content transition=%#v", transition)
	}
	reasoning, _ := canonical.NewReasoningSummary(itemID, []canonical.TextContent{text})
	item, _ := canonical.NewReasoningSummaryItem(reasoning)
	itemDone, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, 0, item)
	b.mustAccept(itemDone)
}
