package canonical

import (
	"strings"
	"testing"
)

func testSearchAction(t *testing.T) HostedSearchAction {
	t.Helper()
	action, err := NewHostedSearchActionSearch([]string{"ant router", "codex tools"}, []string{"https://example.com/source"})
	if err != nil {
		t.Fatal(err)
	}
	return action
}

func TestHostedSearchActionVariants(t *testing.T) {
	search := testSearchAction(t)
	if !search.Valid() || search.Kind() != HostedSearchActionKindSearch || len(search.Queries()) != 2 || len(search.Sources()) != 1 {
		t.Fatalf("search action = %#v", search)
	}
	queries := search.Queries()
	queries[0] = "changed"
	if search.Queries()[0] != "ant router" {
		t.Fatal("search queries were not immutable")
	}
	open, err := NewHostedSearchActionOpenPage("https://example.com/page?q=1#section")
	if err != nil || !open.Valid() || open.Kind() != HostedSearchActionKindOpenPage || open.URL() == "" {
		t.Fatalf("open action = %#v, err = %v", open, err)
	}
	find, err := NewHostedSearchActionFindInPage("https://example.com/page", "router")
	if err != nil || !find.Valid() || find.Kind() != HostedSearchActionKindFindInPage || find.Pattern() != "router" {
		t.Fatalf("find action = %#v, err = %v", find, err)
	}
}

func TestHostedSearchActionRejectsInvalid(t *testing.T) {
	tests := []struct {
		make func() error
		code ErrorCode
	}{
		{func() error { _, err := NewHostedSearchActionSearch(nil, nil); return err }, CodeInvalidInvariant},
		{func() error { _, err := NewHostedSearchActionSearch([]string{string([]byte{0xff})}, nil); return err }, CodeInvalidUTF8},
		{func() error { _, err := NewHostedSearchActionSearch([]string{"bad\x00query"}, nil); return err }, CodeInvalidInvariant},
		{func() error {
			_, err := NewHostedSearchActionSearch([]string{strings.Repeat("q", MaxHostedSearchQueryBytes+1)}, nil)
			return err
		}, CodeLimitExceeded},
		{func() error {
			_, err := NewHostedSearchActionSearch(make([]string, MaxHostedSearchQueries+1), nil)
			return err
		}, CodeLimitExceeded},
		{func() error {
			_, err := NewHostedSearchActionSearch([]string{"query"}, make([]string, MaxHostedSearchSources+1))
			return err
		}, CodeLimitExceeded},
		{func() error {
			_, err := NewHostedSearchActionSearch([]string{"query"}, []string{"http://example.com"})
			return err
		}, CodeInvalidInvariant},
		{func() error {
			_, err := NewHostedSearchActionOpenPage(strings.Repeat("x", MaxHostedSearchURLBytes+1))
			return err
		}, CodeLimitExceeded},
		{func() error { _, err := NewHostedSearchActionOpenPage("https://user@example.com/page"); return err }, CodeInvalidInvariant},
		{func() error { _, err := NewHostedSearchActionFindInPage("https://example.com", ""); return err }, CodeInvalidInvariant},
		{func() error {
			_, err := NewHostedSearchActionFindInPage("https://example.com", string([]byte{0xff}))
			return err
		}, CodeInvalidUTF8},
		{func() error {
			_, err := NewHostedSearchActionFindInPage("https://example.com", strings.Repeat("p", MaxHostedSearchPatternBytes+1))
			return err
		}, CodeLimitExceeded},
	}
	for index, test := range tests {
		if err := test.make(); codeOfCanonical(err) != test.code {
			t.Fatalf("invalid action %d code = %q, want %q; error = %v", index, codeOfCanonical(err), test.code, err)
		}
	}
}

func TestHostedSearchCallItemAndRequest(t *testing.T) {
	action := testSearchAction(t)
	call, err := NewHostedSearchCall(testItemID(t, 110), HostedSearchStatusCompleted, action)
	if err != nil || !call.Valid() || call.Status() != HostedSearchStatusCompleted {
		t.Fatalf("search call = %#v, err = %v", call, err)
	}
	item, err := NewHostedSearchCallItem(call)
	if err != nil || !item.Valid() || item.Type() != ItemType("hosted_search_call") || item.ID() != call.ID() {
		t.Fatalf("search item = %#v, err = %v", item, err)
	}
	got, ok := item.HostedSearchCall()
	if !ok || got.Action().Kind() != HostedSearchActionKindSearch {
		t.Fatalf("search accessor = %#v, %v", got, ok)
	}
	request, err := NewRequest(RequestParams{ID: testRequestID(t, 110), Model: "model", Input: []Item{item}, Tools: []ToolDefinition{NewHostedWebSearchTool()}})
	if err != nil || !request.Capabilities().Has(CapabilityHostedWebSearch) || !request.Capabilities().Has(CapabilityTools) {
		t.Fatalf("search request = %#v, err = %v", request, err)
	}
	if _, err := NewRequest(RequestParams{ID: testRequestID(t, 111), Model: "model", Input: []Item{item}}); err == nil {
		t.Fatal("search call accepted without hosted-search declaration")
	}
	if _, err := NewResponse(ResponseParams{ID: testResponseID(t, 110), Model: "model", Items: []Item{item}, FinishReason: FinishReason("stop")}); err != nil {
		t.Fatalf("terminal search response rejected: %v", err)
	}
}

func TestHostedSearchCallRequiresTerminalStatus(t *testing.T) {
	action := testSearchAction(t)
	for _, status := range []HostedSearchStatus{HostedSearchStatusInProgress, HostedSearchStatusSearching} {
		if _, err := NewHostedSearchCall(testItemID(t, 112), status, action); err == nil {
			t.Fatalf("nonterminal status %q accepted", status)
		}
	}
	for _, status := range []HostedSearchStatus{HostedSearchStatusCompleted, HostedSearchStatusFailed} {
		if call, err := NewHostedSearchCall(testItemID(t, 113), status, action); err != nil || !call.Valid() {
			t.Fatalf("terminal status %q rejected: %v", status, err)
		}
	}
}

func TestHostedSearchEvents(t *testing.T) {
	action := testSearchAction(t)
	started, err := NewItemStartedEvent(testEventID(t, 114), testResponseID(t, 114), 0, ItemStartParams{Index: 0, ID: testItemID(t, 114), Type: ItemType("hosted_search_call"), SearchStatus: HostedSearchStatusInProgress, SearchAction: action})
	if err != nil || !started.Valid() {
		t.Fatalf("search start = %#v, err = %v", started, err)
	}
	startPayload, _ := started.ItemStarted()
	if startPayload.SearchStatus() != HostedSearchStatusInProgress || startPayload.SearchAction().Kind() != HostedSearchActionKindSearch {
		t.Fatalf("search start payload = %#v", startPayload)
	}
	statusEvent, err := NewHostedSearchStatusEvent(testEventID(t, 115), testResponseID(t, 114), 1, testItemID(t, 114), HostedSearchStatusSearching)
	if err != nil || !statusEvent.Valid() || statusEvent.Type() != EventType("hosted_search_status") {
		t.Fatalf("search status event = %#v, err = %v", statusEvent, err)
	}
	payload, ok := statusEvent.HostedSearchStatus()
	if !ok || payload.Status() != HostedSearchStatusSearching {
		t.Fatalf("search status payload = %#v, %v", payload, ok)
	}
}

func TestHostedSearchStartRejectsTerminalOrMissingAction(t *testing.T) {
	params := ItemStartParams{Index: 0, ID: testItemID(t, 118), Type: ItemType("hosted_search_call"), SearchStatus: HostedSearchStatusCompleted, SearchAction: testSearchAction(t)}
	if _, err := NewItemStartedEvent(testEventID(t, 118), testResponseID(t, 118), 0, params); err == nil {
		t.Fatal("terminal hosted-search start accepted")
	}
	params.SearchStatus = HostedSearchStatusInProgress
	params.SearchAction = HostedSearchAction{}
	if _, err := NewItemStartedEvent(testEventID(t, 119), testResponseID(t, 119), 0, params); err == nil {
		t.Fatal("hosted-search start without action accepted")
	}
}

func TestHostedSearchErrorsRedactMetadata(t *testing.T) {
	const secret = "search-secret-a7515bf078594be3"
	_, err := NewHostedSearchActionSearch([]string{secret + "\x00"}, nil)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("search metadata leaked in error: %v", err)
	}
}

func FuzzHostedSearchActions(f *testing.F) {
	f.Add("search", "query", "https://example.com", "")
	f.Add("find_in_page", "", "http://example.com", "pattern")
	f.Fuzz(func(t *testing.T, kind, query, reference, pattern string) {
		if len(kind) > 32 || len(query) > MaxHostedSearchQueryBytes+1 || len(reference) > MaxHostedSearchURLBytes+1 || len(pattern) > MaxHostedSearchPatternBytes+1 {
			return
		}
		var action HostedSearchAction
		var err error
		switch HostedSearchActionKind(kind) {
		case HostedSearchActionKindSearch:
			action, err = NewHostedSearchActionSearch([]string{query}, []string{reference})
		case HostedSearchActionKindOpenPage:
			action, err = NewHostedSearchActionOpenPage(reference)
		case HostedSearchActionKindFindInPage:
			action, err = NewHostedSearchActionFindInPage(reference, pattern)
		default:
			return
		}
		if err == nil && !action.Valid() {
			t.Fatal("constructor returned invalid hosted-search action")
		}
	})
}
