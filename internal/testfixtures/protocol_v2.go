// Package testfixtures holds canonical protocol fixtures shared by the
// canonical and streaming test suites. The v2 event fixture forms complete
// legal lifecycles for separate items so it can be replayed through the
// streaming machine and reconstructed into the expected response.
package testfixtures

import (
	"bytes"
	"io"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func eventID(t *testing.T, value byte) canonical.EventID {
	t.Helper()
	id, err := canonical.GenerateEventID(reader(value))
	if err != nil {
		t.Fatalf("GenerateEventID() failed: %v", err)
	}
	return id
}

func responseID(t *testing.T, value byte) canonical.ResponseID {
	t.Helper()
	id, err := canonical.GenerateResponseID(reader(value))
	if err != nil {
		t.Fatalf("GenerateResponseID() failed: %v", err)
	}
	return id
}

func requestID(t *testing.T, value byte) canonical.RequestID {
	t.Helper()
	id, err := canonical.GenerateRequestID(reader(value))
	if err != nil {
		t.Fatalf("GenerateRequestID() failed: %v", err)
	}
	return id
}

func itemID(t *testing.T, value byte) canonical.ItemID {
	t.Helper()
	id, err := canonical.GenerateItemID(reader(value))
	if err != nil {
		t.Fatalf("GenerateItemID() failed: %v", err)
	}
	return id
}

func callID(t *testing.T, value byte) canonical.ToolCallID {
	t.Helper()
	id, err := canonical.GenerateToolCallID(reader(value))
	if err != nil {
		t.Fatalf("GenerateToolCallID() failed: %v", err)
	}
	return id
}

func reader(value byte) io.Reader {
	return bytes.NewReader(bytes.Repeat([]byte{value}, canonical.IDRandomBytes))
}

// ProtocolV2Request builds the v2 request fixture exercising custom and
// hosted-search tools.
func ProtocolV2Request(t *testing.T, customInput string) canonical.Request {
	t.Helper()
	custom, err := canonical.NewCustomTool("apply_patch", "freeform tool")
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := canonical.NewToolNamespace("workspace", "", []canonical.ToolDefinition{custom})
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewNamespacedCustomToolCall(itemID(t, 120), callID(t, 120), "workspace", "apply_patch", customInput)
	if err != nil {
		t.Fatal(err)
	}
	callItem, err := canonical.NewCustomToolCallItem(call)
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResult(itemID(t, 121), call.CallID(), []canonical.ContentBlock{protocolV2TextBlock(t, "done", nil)}, false)
	if err != nil {
		t.Fatal(err)
	}
	resultItem, err := canonical.NewToolResultItem(result)
	if err != nil {
		t.Fatal(err)
	}
	action, err := canonical.NewHostedSearchActionSearch([]string{"ant router"}, []string{"https://example.com/source"})
	if err != nil {
		t.Fatal(err)
	}
	search, err := canonical.NewHostedSearchCall(itemID(t, 122), canonical.HostedSearchStatusCompleted, action)
	if err != nil {
		t.Fatal(err)
	}
	searchItem, err := canonical.NewHostedSearchCallItem(search)
	if err != nil {
		t.Fatal(err)
	}
	choice, err := canonical.NewNamespacedToolChoice("workspace", "apply_patch")
	if err != nil {
		t.Fatal(err)
	}
	request, err := canonical.NewRequest(canonical.RequestParams{ID: requestID(t, 120), Model: "model", Input: []canonical.Item{callItem, resultItem, searchItem}, Tools: []canonical.ToolDefinition{namespace, canonical.NewHostedWebSearchTool()}, ToolChoice: choice})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

// ProtocolV2Response builds the v2 response fixture with a cited message,
// a completed hosted search, and a custom tool call. The citationURL is
// parameterized so digest tests can vary it.
func ProtocolV2Response(t *testing.T, citationURL string) canonical.Response {
	t.Helper()
	citation, err := canonical.NewURLCitation(0, 4, "Example", citationURL)
	if err != nil {
		t.Fatal(err)
	}
	text, err := canonical.NewTextContentWithCitations("test output", []canonical.URLCitation{citation})
	if err != nil {
		t.Fatal(err)
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessage(itemID(t, 123), canonical.Role("assistant"), []canonical.ContentBlock{block})
	if err != nil {
		t.Fatal(err)
	}
	messageItem, err := canonical.NewMessageItem(message)
	if err != nil {
		t.Fatal(err)
	}
	customCall, err := canonical.NewCustomToolCall(itemID(t, 124), callID(t, 124), "apply_patch", "patch")
	if err != nil {
		t.Fatal(err)
	}
	customItem, err := canonical.NewCustomToolCallItem(customCall)
	if err != nil {
		t.Fatal(err)
	}
	action, err := canonical.NewHostedSearchActionOpenPage("https://example.com/page")
	if err != nil {
		t.Fatal(err)
	}
	search, err := canonical.NewHostedSearchCall(itemID(t, 125), canonical.HostedSearchStatusCompleted, action)
	if err != nil {
		t.Fatal(err)
	}
	searchItem, err := canonical.NewHostedSearchCallItem(search)
	if err != nil {
		t.Fatal(err)
	}
	response, err := canonical.NewResponse(canonical.ResponseParams{ID: responseID(t, 123), Model: "model", Items: []canonical.Item{messageItem, searchItem, customItem}, FinishReason: canonical.FinishReason("tool_call")})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

// ProtocolV2Expectations describes the deterministic fixture identities so
// consumers can assert reconstruction without duplicating fixture constants.
type ProtocolV2Expectations struct {
	ResponseID    canonical.ResponseID
	SearchItemID  canonical.ItemID
	CustomItemID  canonical.ItemID
	CustomCallID  canonical.ToolCallID
	MessageItemID canonical.ItemID
	CustomInput   string
	MessageText   string
	ItemIDs       []canonical.ItemID
}

// ProtocolV2EventExpectations returns the fixture identities that the event
// stream reconstructs.
func ProtocolV2EventExpectations(t *testing.T) ProtocolV2Expectations {
	t.Helper()
	return ProtocolV2Expectations{
		ResponseID:    responseID(t, 126),
		SearchItemID:  itemID(t, 126),
		CustomItemID:  itemID(t, 127),
		CustomCallID:  callID(t, 127),
		MessageItemID: itemID(t, 128),
		CustomInput:   "patch",
		MessageText:   "Example",
		ItemIDs:       []canonical.ItemID{itemID(t, 126), itemID(t, 127), itemID(t, 128)},
	}
}

// ProtocolV2Events builds the v2 event fixture as one complete legal stream:
// a hosted search item reaches a terminal status, a custom tool call streams
// its input to completion, and a cited message item starts, streams, and
// completes before every item completes and the response terminates.
func ProtocolV2Events(t *testing.T) []canonical.Event {
	t.Helper()
	expectations := ProtocolV2EventExpectations(t)
	action, err := canonical.NewHostedSearchActionSearch([]string{"ant router"}, []string{"https://example.com/source"})
	if err != nil {
		t.Fatal(err)
	}
	citation, err := canonical.NewURLCitation(0, 4, "Example", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	// Each event in the fixture must carry a unique ID, mirroring the
	// duplicate-ID rejection the streaming machine enforces.
	unique := func(sequence uint64) canonical.EventID {
		return eventID(t, byte(126+sequence))
	}
	searchStart, err := canonical.NewItemStartedEvent(unique(1), expectations.ResponseID, 1, canonical.ItemStartParams{Index: 0, ID: expectations.SearchItemID, Type: canonical.ItemType("hosted_search_call"), SearchStatus: canonical.HostedSearchStatusInProgress, SearchAction: action})
	if err != nil {
		t.Fatal(err)
	}
	searching, err := canonical.NewHostedSearchStatusEvent(unique(2), expectations.ResponseID, 2, expectations.SearchItemID, canonical.HostedSearchStatusSearching)
	if err != nil {
		t.Fatal(err)
	}
	searchDone, err := canonical.NewHostedSearchStatusEvent(unique(3), expectations.ResponseID, 3, expectations.SearchItemID, canonical.HostedSearchStatusCompleted)
	if err != nil {
		t.Fatal(err)
	}
	customStart, err := canonical.NewItemStartedEvent(unique(4), expectations.ResponseID, 4, canonical.ItemStartParams{Index: 1, ID: expectations.CustomItemID, Type: canonical.ItemType("custom_tool_call"), CallID: expectations.CustomCallID, Namespace: "workspace", Name: "apply_patch"})
	if err != nil {
		t.Fatal(err)
	}
	customDelta, err := canonical.NewCustomInputDeltaEvent(unique(5), expectations.ResponseID, 5, expectations.CustomItemID, expectations.CustomInput)
	if err != nil {
		t.Fatal(err)
	}
	customDone, err := canonical.NewCustomInputCompletedEvent(unique(6), expectations.ResponseID, 6, expectations.CustomItemID, expectations.CustomInput)
	if err != nil {
		t.Fatal(err)
	}
	messageStart, err := canonical.NewItemStartedEvent(unique(7), expectations.ResponseID, 7, canonical.ItemStartParams{Index: 2, ID: expectations.MessageItemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	if err != nil {
		t.Fatal(err)
	}
	contentStart, err := canonical.NewContentStartedEvent(unique(8), expectations.ResponseID, 8, expectations.MessageItemID, 0, canonical.ContentType("text"))
	if err != nil {
		t.Fatal(err)
	}
	textDelta, err := canonical.NewTextDeltaEvent(unique(9), expectations.ResponseID, 9, expectations.MessageItemID, 0, expectations.MessageText)
	if err != nil {
		t.Fatal(err)
	}
	citationAdded, err := canonical.NewURLCitationAddedEvent(unique(10), expectations.ResponseID, 10, expectations.MessageItemID, 0, citation)
	if err != nil {
		t.Fatal(err)
	}
	contentDone, err := canonical.NewContentCompletedEvent(unique(11), expectations.ResponseID, 11, expectations.MessageItemID, 0, protocolV2TextBlock(t, expectations.MessageText, []canonical.URLCitation{citation}))
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessage(expectations.MessageItemID, canonical.Role("assistant"), []canonical.ContentBlock{protocolV2TextBlock(t, expectations.MessageText, []canonical.URLCitation{citation})})
	if err != nil {
		t.Fatal(err)
	}
	messageItem, err := canonical.NewMessageItem(message)
	if err != nil {
		t.Fatal(err)
	}
	messageCompleted, err := canonical.NewItemCompletedEvent(unique(12), expectations.ResponseID, 12, 2, messageItem)
	if err != nil {
		t.Fatal(err)
	}
	customCall, err := canonical.NewNamespacedCustomToolCall(expectations.CustomItemID, expectations.CustomCallID, "workspace", "apply_patch", expectations.CustomInput)
	if err != nil {
		t.Fatal(err)
	}
	customItem, err := canonical.NewCustomToolCallItem(customCall)
	if err != nil {
		t.Fatal(err)
	}
	customCompleted, err := canonical.NewItemCompletedEvent(unique(13), expectations.ResponseID, 13, 1, customItem)
	if err != nil {
		t.Fatal(err)
	}
	search, err := canonical.NewHostedSearchCall(expectations.SearchItemID, canonical.HostedSearchStatusCompleted, action)
	if err != nil {
		t.Fatal(err)
	}
	searchItem, err := canonical.NewHostedSearchCallItem(search)
	if err != nil {
		t.Fatal(err)
	}
	searchCompleted, err := canonical.NewItemCompletedEvent(unique(14), expectations.ResponseID, 14, 0, searchItem)
	if err != nil {
		t.Fatal(err)
	}
	responseDone, err := canonical.NewResponseCompletedEvent(unique(15), expectations.ResponseID, 15, canonical.FinishReason("tool_call"), canonical.Usage{}, expectations.ItemIDs)
	if err != nil {
		t.Fatal(err)
	}
	started, err := canonical.NewResponseStartedEvent(unique(0), expectations.ResponseID, 0, "model")
	if err != nil {
		t.Fatal(err)
	}
	return []canonical.Event{started, searchStart, searching, searchDone, customStart, customDelta, customDone, messageStart, contentStart, textDelta, citationAdded, contentDone, messageCompleted, customCompleted, searchCompleted, responseDone}
}

func protocolV2TextBlock(t *testing.T, text string, citations []canonical.URLCitation) canonical.ContentBlock {
	t.Helper()
	var content canonical.TextContent
	var err error
	if len(citations) == 0 {
		content, err = canonical.NewTextContent(text)
	} else {
		content, err = canonical.NewTextContentWithCitations(text, citations)
	}
	if err != nil {
		t.Fatal(err)
	}
	block, err := canonical.NewTextBlock(content)
	if err != nil {
		t.Fatal(err)
	}
	return block
}
