package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func searchAction(t *testing.T) canonical.HostedSearchAction {
	t.Helper()
	action, err := canonical.NewHostedSearchActionSearch([]string{"ant router"}, []string{"https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return action
}

func startSearchItem(t *testing.T, b *streamBuilder, itemID canonical.ItemID) Transition {
	t.Helper()
	start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: len(b.machine.itemOrder), ID: itemID, Type: canonical.ItemType("hosted_search_call"), SearchStatus: canonical.HostedSearchStatusInProgress, SearchAction: searchAction(t)})
	b.sequence++
	return b.mustAccept(start)
}

func TestMachineReconstructsHostedSearchAndCommitsAtStart(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 40)
	transition := startSearchItem(t, b, itemID)
	if !transition.SemanticOutput() || !transition.JustCommitted() || b.machine.State() != StateCommitted {
		t.Fatalf("search start transition = %#v", transition)
	}
	for _, status := range []canonical.HostedSearchStatus{canonical.HostedSearchStatusSearching, canonical.HostedSearchStatusCompleted} {
		event, _ := canonical.NewHostedSearchStatusEvent(b.nextEventID(), b.responseID, b.sequence, itemID, status)
		b.sequence++
		if transition := b.mustAccept(event); !transition.SemanticOutput() || transition.JustCommitted() {
			t.Fatalf("status %q transition = %#v", status, transition)
		}
	}
	call, _ := canonical.NewHostedSearchCall(itemID, canonical.HostedSearchStatusCompleted, searchAction(t))
	item, _ := canonical.NewHostedSearchCallItem(call)
	itemDone, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, 0, item)
	b.sequence++
	b.mustAccept(itemDone)
	terminal, _ := canonical.NewResponseCompletedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.FinishReason("stop"), canonical.Usage{}, []canonical.ItemID{itemID})
	responseTransition := b.mustAccept(terminal)
	response, ok := responseTransition.Response()
	if !ok || !response.Valid() || response.Items()[0].Type() != canonical.ItemType("hosted_search_call") {
		t.Fatalf("terminal search response = %#v, %v", response, ok)
	}
}

func TestMachineHostedSearchActionCountsTowardResponseLimit(t *testing.T) {
	const actionBytes = len("ant router") + len("https://example.com")

	t.Run("exact limit", func(t *testing.T) {
		options := DefaultMachineOptions()
		options.MaxResponseBytes = actionBytes
		b := newStreamBuilder(t, options)
		itemID := streamItemID(t, 43)
		startSearchItem(t, b, itemID)
		if got := b.machine.Snapshot().ReconstructedBytes(); got != actionBytes {
			t.Fatalf("reconstructed bytes = %d, want %d", got, actionBytes)
		}
	})

	t.Run("limit exceeded is transactional", func(t *testing.T) {
		options := DefaultMachineOptions()
		options.MaxResponseBytes = actionBytes - 1
		b := newStreamBuilder(t, options)
		itemID := streamItemID(t, 44)
		start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("hosted_search_call"), SearchStatus: canonical.HostedSearchStatusInProgress, SearchAction: searchAction(t)})
		before := b.machine.Snapshot()
		if _, err := b.machine.Accept(start); codeOfStream(err) != CodeResponseLimitExceeded || b.machine.Snapshot() != before {
			t.Fatalf("limit code=%q snapshot=%#v", codeOfStream(err), b.machine.Snapshot())
		}
	})
}

func TestMachineRejectsHostedSearchStatusRegressionTransactionally(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 41)
	startSearchItem(t, b, itemID)
	searching, _ := canonical.NewHostedSearchStatusEvent(b.nextEventID(), b.responseID, b.sequence, itemID, canonical.HostedSearchStatusSearching)
	b.sequence++
	b.mustAccept(searching)
	for _, status := range []canonical.HostedSearchStatus{canonical.HostedSearchStatusSearching, canonical.HostedSearchStatusInProgress} {
		event, _ := canonical.NewHostedSearchStatusEvent(b.nextEventID(), b.responseID, b.sequence, itemID, status)
		before := b.machine.Snapshot()
		if _, err := b.machine.Accept(event); codeOfStream(err) != CodeInvalidItemState || b.machine.Snapshot() != before {
			t.Fatalf("status %q code=%q snapshot=%#v", status, codeOfStream(err), b.machine.Snapshot())
		}
	}
}

func TestMachineRejectsHostedSearchCompletionMismatch(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 42)
	startSearchItem(t, b, itemID)
	completed, _ := canonical.NewHostedSearchStatusEvent(b.nextEventID(), b.responseID, b.sequence, itemID, canonical.HostedSearchStatusCompleted)
	b.sequence++
	b.mustAccept(completed)
	wrongAction, _ := canonical.NewHostedSearchActionOpenPage("https://example.com/other")
	call, _ := canonical.NewHostedSearchCall(itemID, canonical.HostedSearchStatusCompleted, wrongAction)
	item, _ := canonical.NewHostedSearchCallItem(call)
	itemDone, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, 0, item)
	before := b.machine.Snapshot()
	if _, err := b.machine.Accept(itemDone); codeOfStream(err) != CodeCompletionMismatch || b.machine.Snapshot() != before {
		t.Fatalf("completion code=%q snapshot=%#v", codeOfStream(err), b.machine.Snapshot())
	}
}
