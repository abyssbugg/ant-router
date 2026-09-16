package canonical_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/testfixtures"
)

func codeOfProtocolV2(err error) canonical.ErrorCode {
	code, _ := canonical.CodeOf(err)
	return code
}

func assertGolden(t *testing.T, name string, encoded []byte) {
	t.Helper()
	path := filepath.Join("testdata", "v2", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("os.MkdirAll() failed: %v", err)
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			t.Fatalf("os.WriteFile(%s) failed: %v", name, err)
		}
	}
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) failed: %v", name, err)
	}
	if !bytes.Equal(golden, encoded) {
		t.Errorf("fixture %s differs; run UPDATE_GOLDEN=1", name)
	}
}

func TestProtocolV2VersionConstants(t *testing.T) {
	if canonical.ProtocolVersionV1 != "1" || canonical.ProtocolVersion != "2" {
		t.Fatalf("protocol versions = %q, %q", canonical.ProtocolVersionV1, canonical.ProtocolVersion)
	}
}

func TestProtocolV2RequestRoundTrip(t *testing.T) {
	request := testfixtures.ProtocolV2Request(t, "patch")
	encoded, err := canonical.EncodeRequest(request)
	if err != nil || !bytes.Contains(encoded, []byte(`"version":"2"`)) || !bytes.Contains(encoded, []byte(`"type":"custom"`)) || !bytes.Contains(encoded, []byte(`"type":"hosted_search_call"`)) {
		t.Fatalf("encoded request = %s, err = %v", encoded, err)
	}
	decoded, err := canonical.DecodeRequest(encoded)
	if err != nil || !decoded.Valid() {
		t.Fatalf("decoded request = %#v, err = %v", decoded, err)
	}
	reencoded, _ := canonical.EncodeRequest(decoded)
	if !bytes.Equal(encoded, reencoded) {
		t.Fatal("v2 request round trip changed bytes")
	}
}

func TestProtocolV2ResponseRoundTrip(t *testing.T) {
	encoded, err := canonical.EncodeResponse(testfixtures.ProtocolV2Response(t, "https://example.com/citation"))
	if err != nil || !bytes.Contains(encoded, []byte(`"citations"`)) || !bytes.Contains(encoded, []byte(`"type":"hosted_search_call"`)) {
		t.Fatalf("encoded response = %s, err = %v", encoded, err)
	}
	decoded, err := canonical.DecodeResponse(encoded)
	if err != nil || !decoded.Valid() {
		t.Fatalf("decoded response = %#v, err = %v", decoded, err)
	}
	reencoded, _ := canonical.EncodeResponse(decoded)
	if !bytes.Equal(encoded, reencoded) {
		t.Fatal("v2 response round trip changed bytes")
	}
}

func TestProtocolV2EventRoundTrip(t *testing.T) {
	for _, event := range testfixtures.ProtocolV2Events(t) {
		encoded, err := canonical.EncodeEvent(event)
		if err != nil || !bytes.Contains(encoded, []byte(`"version":"2"`)) {
			t.Fatalf("encode %q = %s, %v", event.Type(), encoded, err)
		}
		decoded, err := canonical.DecodeEvent(encoded)
		if err != nil || !decoded.Valid() || decoded.Type() != event.Type() {
			t.Fatalf("decode %q = %#v, %v", event.Type(), decoded, err)
		}
		reencoded, _ := canonical.EncodeEvent(decoded)
		if !bytes.Equal(encoded, reencoded) {
			t.Fatalf("event %q round trip changed bytes", event.Type())
		}
	}
}

func TestProtocolV2GoldenFixtures(t *testing.T) {
	request, err := canonical.EncodeRequest(testfixtures.ProtocolV2Request(t, "patch"))
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "request_custom_search.json", request)
	response, err := canonical.EncodeResponse(testfixtures.ProtocolV2Response(t, "https://example.com/citation"))
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "response_custom_search_citation.json", response)
	assertGolden(t, "event_stream_custom_search.json", protocolV2EventStream(t))
}

func protocolV2EventStream(t *testing.T) []byte {
	t.Helper()
	encoded := make([]json.RawMessage, 0)
	for _, event := range testfixtures.ProtocolV2Events(t) {
		data, err := canonical.EncodeEvent(event)
		if err != nil {
			t.Fatalf("EncodeEvent(%q) failed: %v", event.Type(), err)
		}
		encoded = append(encoded, data)
	}
	data, err := json.Marshal(encoded)
	if err != nil {
		t.Fatalf("json.Marshal(events) failed: %v", err)
	}
	return data
}

func TestProtocolV1FixturesRemainDecodable(t *testing.T) {
	for _, file := range []string{"request_text.json", "request_tools.json", "response_complete.json"} {
		data, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		switch file {
		case "response_complete.json":
			if _, err := canonical.DecodeResponse(data); err != nil {
				t.Fatalf("decode legacy %s: %v", file, err)
			}
		default:
			if _, err := canonical.DecodeRequest(data); err != nil {
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
		if _, err := canonical.DecodeEvent(event); err != nil {
			t.Fatalf("decode legacy event %d: %v", index, err)
		}
	}
}

func TestProtocolV1RejectsV2Variants(t *testing.T) {
	request, _ := canonical.EncodeRequest(testfixtures.ProtocolV2Request(t, "patch"))
	request = bytes.Replace(request, []byte(`"version":"2"`), []byte(`"version":"1"`), 1)
	if _, err := canonical.DecodeRequest(request); codeOfProtocolV2(err) != canonical.CodeInvalidVariant {
		t.Fatalf("v1 custom request code = %q, error = %v", codeOfProtocolV2(err), err)
	}
	response, _ := canonical.EncodeResponse(testfixtures.ProtocolV2Response(t, "https://example.com/citation"))
	response = bytes.Replace(response, []byte(`"version":"2"`), []byte(`"version":"1"`), 1)
	if _, err := canonical.DecodeResponse(response); codeOfProtocolV2(err) != canonical.CodeInvalidVariant {
		t.Fatalf("v1 cited response code = %q, error = %v", codeOfProtocolV2(err), err)
	}
	event, _ := canonical.EncodeEvent(testfixtures.ProtocolV2Events(t)[2])
	event = bytes.Replace(event, []byte(`"version":"2"`), []byte(`"version":"1"`), 1)
	if _, err := canonical.DecodeEvent(event); codeOfProtocolV2(err) != canonical.CodeInvalidVariant {
		t.Fatalf("v1 search event code = %q, error = %v", codeOfProtocolV2(err), err)
	}
}

func TestProtocolV2RejectsUnknownNestedFields(t *testing.T) {
	request, err := canonical.EncodeRequest(testfixtures.ProtocolV2Request(t, "patch"))
	if err != nil {
		t.Fatal(err)
	}
	request = bytes.Replace(request, []byte(`"custom_format":{"type":"text"}`), []byte(`"custom_format":{"type":"text","extra":true}`), 1)
	if _, err := canonical.DecodeRequest(request); codeOfProtocolV2(err) != canonical.CodeUnknownField {
		t.Fatalf("custom format unknown field code = %q, error = %v", codeOfProtocolV2(err), err)
	}
	response, err := canonical.EncodeResponse(testfixtures.ProtocolV2Response(t, "https://example.com/citation"))
	if err != nil {
		t.Fatal(err)
	}
	citation := bytes.Replace(response, []byte(`"url":"https://example.com/citation"}`), []byte(`"url":"https://example.com/citation","extra":true}`), 1)
	if _, err := canonical.DecodeResponse(citation); codeOfProtocolV2(err) != canonical.CodeUnknownField {
		t.Fatalf("citation unknown field code = %q, error = %v", codeOfProtocolV2(err), err)
	}
	response, err = canonical.EncodeResponse(testfixtures.ProtocolV2Response(t, "https://example.com/page"))
	if err != nil {
		t.Fatal(err)
	}
	action := bytes.Replace(response, []byte(`"url":"https://example.com/page"}`), []byte(`"url":"https://example.com/page","extra":true}`), 1)
	if _, err := canonical.DecodeResponse(action); codeOfProtocolV2(err) != canonical.CodeUnknownField {
		t.Fatalf("search action unknown field code = %q, error = %v", codeOfProtocolV2(err), err)
	}
	event, err := canonical.EncodeEvent(testfixtures.ProtocolV2Events(t)[2])
	if err != nil {
		t.Fatal(err)
	}
	event = bytes.Replace(event, []byte(`"status":"searching"}`), []byte(`"status":"searching","extra":true}`), 1)
	if _, err := canonical.DecodeEvent(event); codeOfProtocolV2(err) != canonical.CodeUnknownField {
		t.Fatalf("event payload unknown field code = %q, error = %v", codeOfProtocolV2(err), err)
	}
}

func TestProtocolV2SemanticDigestsIncludeNewValues(t *testing.T) {
	first, err := canonical.FullDigest(testfixtures.ProtocolV2Request(t, "patch one"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := canonical.FullDigest(testfixtures.ProtocolV2Request(t, "patch two"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("custom input did not change request digest")
	}
	firstPortable, err := canonical.PortableDigest(testfixtures.ProtocolV2Request(t, "patch one"))
	if err != nil {
		t.Fatal(err)
	}
	secondPortable, err := canonical.PortableDigest(testfixtures.ProtocolV2Request(t, "patch two"))
	if err != nil {
		t.Fatal(err)
	}
	if firstPortable == secondPortable {
		t.Fatal("portable digest ignored custom input")
	}
	firstResponse, err := canonical.FullResponseDigest(testfixtures.ProtocolV2Response(t, "https://example.com/one"))
	if err != nil {
		t.Fatal(err)
	}
	secondResponse, err := canonical.FullResponseDigest(testfixtures.ProtocolV2Response(t, "https://example.com/two"))
	if err != nil {
		t.Fatal(err)
	}
	if firstResponse == secondResponse {
		t.Fatal("citation URL did not change response digest")
	}
	firstPortableResponse, err := canonical.PortableResponseDigest(testfixtures.ProtocolV2Response(t, "https://example.com/one"))
	if err != nil {
		t.Fatal(err)
	}
	secondPortableResponse, err := canonical.PortableResponseDigest(testfixtures.ProtocolV2Response(t, "https://example.com/two"))
	if err != nil {
		t.Fatal(err)
	}
	if firstPortableResponse == secondPortableResponse {
		t.Fatal("portable response digest ignored citation URL")
	}
}
