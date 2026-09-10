package canonical

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func protocolV2Request(t *testing.T, customInput string) Request {
	t.Helper()
	custom := testCustomTool(t, "apply_patch")
	namespace, err := NewToolNamespace("workspace", "", []ToolDefinition{custom})
	if err != nil {
		t.Fatal(err)
	}
	call, _ := NewNamespacedCustomToolCall(testItemID(t, 120), testCallID(t, 120), "workspace", "apply_patch", customInput)
	callItem, _ := NewCustomToolCallItem(call)
	result, _ := NewToolResult(testItemID(t, 121), call.CallID(), []ContentBlock{testTextBlock(t, "done")}, false)
	resultItem, _ := NewToolResultItem(result)
	action, _ := NewHostedSearchActionSearch([]string{"ant router"}, []string{"https://example.com/source"})
	search, _ := NewHostedSearchCall(testItemID(t, 122), HostedSearchStatusCompleted, action)
	searchItem, _ := NewHostedSearchCallItem(search)
	choice, _ := NewNamespacedToolChoice("workspace", "apply_patch")
	request, err := NewRequest(RequestParams{ID: testRequestID(t, 120), Model: "model", Input: []Item{callItem, resultItem, searchItem}, Tools: []ToolDefinition{namespace, NewHostedWebSearchTool()}, ToolChoice: choice})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func protocolV2Response(t *testing.T, citationURL string) Response {
	t.Helper()
	citation, _ := NewURLCitation(0, 4, "Example", citationURL)
	text, _ := NewTextContentWithCitations("test output", []URLCitation{citation})
	block, _ := NewTextBlock(text)
	message, _ := NewMessage(testItemID(t, 123), Role("assistant"), []ContentBlock{block})
	messageItem, _ := NewMessageItem(message)
	customCall, _ := NewCustomToolCall(testItemID(t, 124), testCallID(t, 124), "apply_patch", "patch")
	customItem, _ := NewCustomToolCallItem(customCall)
	action, _ := NewHostedSearchActionOpenPage("https://example.com/page")
	search, _ := NewHostedSearchCall(testItemID(t, 125), HostedSearchStatusCompleted, action)
	searchItem, _ := NewHostedSearchCallItem(search)
	response, err := NewResponse(ResponseParams{ID: testResponseID(t, 123), Model: "model", Items: []Item{messageItem, searchItem, customItem}, FinishReason: FinishReason("tool_call")})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func protocolV2Events(t *testing.T) []Event {
	t.Helper()
	eventID := testEventID(t, 126)
	responseID := testResponseID(t, 126)
	itemID := testItemID(t, 126)
	action := testSearchAction(t)
	started, _ := NewItemStartedEvent(eventID, responseID, 0, ItemStartParams{Index: 0, ID: itemID, Type: ItemType("hosted_search_call"), SearchStatus: HostedSearchStatusInProgress, SearchAction: action})
	searching, _ := NewHostedSearchStatusEvent(eventID, responseID, 1, itemID, HostedSearchStatusSearching)
	citation, _ := NewURLCitation(0, 4, "Example", "https://example.com")
	cited, _ := NewURLCitationAddedEvent(eventID, responseID, 2, itemID, 0, citation)
	customDelta, _ := NewCustomInputDeltaEvent(eventID, responseID, 3, itemID, "patch")
	customDone, _ := NewCustomInputCompletedEvent(eventID, responseID, 4, itemID, "patch")
	return []Event{started, searching, cited, customDelta, customDone}
}

func TestProtocolV2VersionConstants(t *testing.T) {
	if ProtocolVersionV1 != "1" || ProtocolVersion != "2" {
		t.Fatalf("protocol versions = %q, %q", ProtocolVersionV1, ProtocolVersion)
	}
}

func TestProtocolV2RequestRoundTrip(t *testing.T) {
	request := protocolV2Request(t, "patch")
	encoded, err := EncodeRequest(request)
	if err != nil || !bytes.Contains(encoded, []byte(`"version":"2"`)) || !bytes.Contains(encoded, []byte(`"type":"custom"`)) || !bytes.Contains(encoded, []byte(`"type":"hosted_search_call"`)) {
		t.Fatalf("encoded request = %s, err = %v", encoded, err)
	}
	decoded, err := DecodeRequest(encoded)
	if err != nil || !decoded.Valid() {
		t.Fatalf("decoded request = %#v, err = %v", decoded, err)
	}
	reencoded, _ := EncodeRequest(decoded)
	if !bytes.Equal(encoded, reencoded) {
		t.Fatal("v2 request round trip changed bytes")
	}
}

func TestProtocolV2ResponseRoundTrip(t *testing.T) {
	encoded, err := EncodeResponse(protocolV2Response(t, "https://example.com/citation"))
	if err != nil || !bytes.Contains(encoded, []byte(`"citations"`)) || !bytes.Contains(encoded, []byte(`"type":"hosted_search_call"`)) {
		t.Fatalf("encoded response = %s, err = %v", encoded, err)
	}
	decoded, err := DecodeResponse(encoded)
	if err != nil || !decoded.Valid() {
		t.Fatalf("decoded response = %#v, err = %v", decoded, err)
	}
	reencoded, _ := EncodeResponse(decoded)
	if !bytes.Equal(encoded, reencoded) {
		t.Fatal("v2 response round trip changed bytes")
	}
}

func TestProtocolV2EventRoundTrip(t *testing.T) {
	for _, event := range protocolV2Events(t) {
		encoded, err := EncodeEvent(event)
		if err != nil || !bytes.Contains(encoded, []byte(`"version":"2"`)) {
			t.Fatalf("encode %q = %s, %v", event.Type(), encoded, err)
		}
		decoded, err := DecodeEvent(encoded)
		if err != nil || !decoded.Valid() || decoded.Type() != event.Type() {
			t.Fatalf("decode %q = %#v, %v", event.Type(), decoded, err)
		}
		reencoded, _ := EncodeEvent(decoded)
		if !bytes.Equal(encoded, reencoded) {
			t.Fatalf("event %q round trip changed bytes", event.Type())
		}
	}
}

func TestProtocolV2GoldenFixtures(t *testing.T) {
	request, err := EncodeRequest(protocolV2Request(t, "patch"))
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "request_custom_search.json", request)
	response, err := EncodeResponse(protocolV2Response(t, "https://example.com/citation"))
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "response_custom_search_citation.json", response)
	assertGolden(t, "event_stream_custom_search.json", encodeEventStream(t, protocolV2Events(t)))
}

func TestProtocolV1FixturesRemainDecodable(t *testing.T) {
	for _, file := range []string{"request_text.json", "request_tools.json", "response_complete.json"} {
		data, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		switch file {
		case "response_complete.json":
			if _, err := DecodeResponse(data); err != nil {
				t.Fatalf("decode legacy %s: %v", file, err)
			}
		default:
			if _, err := DecodeRequest(data); err != nil {
				t.Fatalf("decode legacy %s: %v", file, err)
			}
		}
	}
	eventData, err := os.ReadFile("testdata/event_stream_complete.json")
	if err != nil {
		t.Fatal(err)
	}
	var events []json.RawMessage
	if err := json.Unmarshal(eventData, &events); err != nil {
		t.Fatal(err)
	}
	for index, event := range events {
		if _, err := DecodeEvent(event); err != nil {
			t.Fatalf("decode legacy event %d: %v", index, err)
		}
	}
}

func TestProtocolV1RejectsV2Variants(t *testing.T) {
	request, _ := EncodeRequest(protocolV2Request(t, "patch"))
	request = bytes.Replace(request, []byte(`"version":"2"`), []byte(`"version":"1"`), 1)
	if _, err := DecodeRequest(request); codeOfCanonical(err) != CodeInvalidVariant {
		t.Fatalf("v1 custom request code = %q, error = %v", codeOfCanonical(err), err)
	}
	response, _ := EncodeResponse(protocolV2Response(t, "https://example.com/citation"))
	response = bytes.Replace(response, []byte(`"version":"2"`), []byte(`"version":"1"`), 1)
	if _, err := DecodeResponse(response); codeOfCanonical(err) != CodeInvalidVariant {
		t.Fatalf("v1 cited response code = %q, error = %v", codeOfCanonical(err), err)
	}
	event, _ := EncodeEvent(protocolV2Events(t)[1])
	event = bytes.Replace(event, []byte(`"version":"2"`), []byte(`"version":"1"`), 1)
	if _, err := DecodeEvent(event); codeOfCanonical(err) != CodeInvalidVariant {
		t.Fatalf("v1 search event code = %q, error = %v", codeOfCanonical(err), err)
	}
}

func TestProtocolV2RejectsUnknownNestedFields(t *testing.T) {
	request, err := EncodeRequest(protocolV2Request(t, "patch"))
	if err != nil {
		t.Fatal(err)
	}
	request = bytes.Replace(request, []byte(`"custom_format":{"type":"text"}`), []byte(`"custom_format":{"type":"text","extra":true}`), 1)
	if _, err := DecodeRequest(request); codeOfCanonical(err) != CodeUnknownField {
		t.Fatalf("custom format unknown field code = %q, error = %v", codeOfCanonical(err), err)
	}
	response, err := EncodeResponse(protocolV2Response(t, "https://example.com/citation"))
	if err != nil {
		t.Fatal(err)
	}
	citation := bytes.Replace(response, []byte(`"url":"https://example.com/citation"}`), []byte(`"url":"https://example.com/citation","extra":true}`), 1)
	if _, err := DecodeResponse(citation); codeOfCanonical(err) != CodeUnknownField {
		t.Fatalf("citation unknown field code = %q, error = %v", codeOfCanonical(err), err)
	}
	action := bytes.Replace(response, []byte(`"url":"https://example.com/page"}`), []byte(`"url":"https://example.com/page","extra":true}`), 1)
	if _, err := DecodeResponse(action); codeOfCanonical(err) != CodeUnknownField {
		t.Fatalf("search action unknown field code = %q, error = %v", codeOfCanonical(err), err)
	}
	event, err := EncodeEvent(protocolV2Events(t)[1])
	if err != nil {
		t.Fatal(err)
	}
	event = bytes.Replace(event, []byte(`"status":"searching"}`), []byte(`"status":"searching","extra":true}`), 1)
	if _, err := DecodeEvent(event); codeOfCanonical(err) != CodeUnknownField {
		t.Fatalf("event payload unknown field code = %q, error = %v", codeOfCanonical(err), err)
	}
}

func TestProtocolV2SemanticDigestsIncludeNewValues(t *testing.T) {
	first, err := FullDigest(protocolV2Request(t, "patch one"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := FullDigest(protocolV2Request(t, "patch two"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("custom input did not change request digest")
	}
	firstPortable, err := PortableDigest(protocolV2Request(t, "patch one"))
	if err != nil {
		t.Fatal(err)
	}
	secondPortable, err := PortableDigest(protocolV2Request(t, "patch two"))
	if err != nil {
		t.Fatal(err)
	}
	if firstPortable == secondPortable {
		t.Fatal("portable digest ignored custom input")
	}
	firstResponse, err := FullResponseDigest(protocolV2Response(t, "https://example.com/one"))
	if err != nil {
		t.Fatal(err)
	}
	secondResponse, err := FullResponseDigest(protocolV2Response(t, "https://example.com/two"))
	if err != nil {
		t.Fatal(err)
	}
	if firstResponse == secondResponse {
		t.Fatal("citation URL did not change response digest")
	}
	firstPortableResponse, err := PortableResponseDigest(protocolV2Response(t, "https://example.com/one"))
	if err != nil {
		t.Fatal(err)
	}
	secondPortableResponse, err := PortableResponseDigest(protocolV2Response(t, "https://example.com/two"))
	if err != nil {
		t.Fatal(err)
	}
	if firstPortableResponse == secondPortableResponse {
		t.Fatal("portable response digest ignored citation URL")
	}
}
