package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func startTextContent(t *testing.T, b *streamBuilder, itemID canonical.ItemID) {
	t.Helper()
	start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: len(b.machine.itemOrder), ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	b.sequence++
	b.mustAccept(start)
	content, _ := canonical.NewContentStartedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, canonical.ContentType("text"))
	b.sequence++
	b.mustAccept(content)
}

func testCitation(t *testing.T) canonical.URLCitation {
	t.Helper()
	citation, err := canonical.NewURLCitation(0, 4, "Example", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	return citation
}

func TestMachineReconstructsCitedText(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 50)
	startTextContent(t, b, itemID)
	delta, _ := canonical.NewTextDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, "test output")
	b.sequence++
	b.mustAccept(delta)
	citationEvent, _ := canonical.NewURLCitationAddedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, testCitation(t))
	b.sequence++
	if transition := b.mustAccept(citationEvent); !transition.SemanticOutput() || transition.JustCommitted() {
		t.Fatalf("citation transition = %#v", transition)
	}
	text, _ := canonical.NewTextContentWithCitations("test output", []canonical.URLCitation{testCitation(t)})
	block, _ := canonical.NewTextBlock(text)
	contentDone, _ := canonical.NewContentCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, block)
	b.sequence++
	b.mustAccept(contentDone)
	message, _ := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
	item, _ := canonical.NewMessageItem(message)
	itemDone, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, 0, item)
	b.sequence++
	b.mustAccept(itemDone)
	terminal, _ := canonical.NewResponseCompletedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.FinishReason("stop"), canonical.Usage{}, []canonical.ItemID{itemID})
	responseTransition := b.mustAccept(terminal)
	response, ok := responseTransition.Response()
	messageValue, _ := response.Items()[0].Message()
	textValue, _ := messageValue.Content()[0].Text()
	if !ok || len(textValue.Citations()) != 1 || textValue.Citations()[0].URL() != "https://example.com" {
		t.Fatalf("cited response = %#v, %v", response, ok)
	}
}

func TestMachineCitationBeforeTextCommits(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 51)
	startTextContent(t, b, itemID)
	event, _ := canonical.NewURLCitationAddedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, testCitation(t))
	b.sequence++
	transition := b.mustAccept(event)
	if !transition.SemanticOutput() || !transition.JustCommitted() {
		t.Fatalf("citation-first transition = %#v", transition)
	}
}

func TestMachineCitationMismatchIsTransactional(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 52)
	startTextContent(t, b, itemID)
	event, _ := canonical.NewURLCitationAddedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, testCitation(t))
	b.sequence++
	b.mustAccept(event)
	text, _ := canonical.NewTextContent("test output")
	block, _ := canonical.NewTextBlock(text)
	contentDone, _ := canonical.NewContentCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, block)
	before := b.machine.Snapshot()
	if _, err := b.machine.Accept(contentDone); codeOfStream(err) != CodeCompletionMismatch || b.machine.Snapshot() != before {
		t.Fatalf("citation mismatch code=%q snapshot=%#v", codeOfStream(err), b.machine.Snapshot())
	}
}

func TestMachineAtomicCitedTextRespectsResponseLimit(t *testing.T) {
	citation := testCitation(t)
	text, _ := canonical.NewTextContentWithCitations("test", []canonical.URLCitation{citation})
	block, _ := canonical.NewTextBlock(text)
	const textBytes = len("test")
	totalBytes := textBytes + citationSize(citation)

	t.Run("exact limit", func(t *testing.T) {
		options := DefaultMachineOptions()
		options.MaxResponseBytes = totalBytes
		b := newStreamBuilder(t, options)
		itemID := streamItemID(t, 54)
		startTextContent(t, b, itemID)
		event, _ := canonical.NewContentCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, block)
		if _, err := b.machine.Accept(event); err != nil || b.machine.Snapshot().ReconstructedBytes() != totalBytes {
			t.Fatalf("exact limit err=%v snapshot=%#v", err, b.machine.Snapshot())
		}
	})

	t.Run("limit exceeded is transactional", func(t *testing.T) {
		options := DefaultMachineOptions()
		options.MaxResponseBytes = totalBytes - 1
		b := newStreamBuilder(t, options)
		itemID := streamItemID(t, 55)
		startTextContent(t, b, itemID)
		event, _ := canonical.NewContentCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, block)
		before := b.machine.Snapshot()
		if _, err := b.machine.Accept(event); codeOfStream(err) != CodeResponseLimitExceeded || b.machine.Snapshot() != before {
			t.Fatalf("limit code=%q snapshot=%#v", codeOfStream(err), b.machine.Snapshot())
		}
	})
}

func TestMachineCitationMetadataCountsTowardResponseLimit(t *testing.T) {
	options := DefaultMachineOptions()
	options.MaxResponseBytes = 4
	b := newStreamBuilder(t, options)
	itemID := streamItemID(t, 53)
	startTextContent(t, b, itemID)
	event, _ := canonical.NewURLCitationAddedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, testCitation(t))
	before := b.machine.Snapshot()
	if _, err := b.machine.Accept(event); codeOfStream(err) != CodeResponseLimitExceeded || b.machine.Snapshot() != before {
		t.Fatalf("citation limit code=%q snapshot=%#v", codeOfStream(err), b.machine.Snapshot())
	}
}
