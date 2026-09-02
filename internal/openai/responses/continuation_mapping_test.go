package responses

import (
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

type fixedAffinityResolver struct {
	providerID, upstreamModel string
	calls                     int
	err                       error
}

func (r *fixedAffinityResolver) ResolveModel(alias string) (string, string, error) {
	r.calls++
	return r.providerID, r.upstreamModel, r.err
}

func TestDecodeAcceptsForcedStatelessControls(t *testing.T) {
	for name, controls := range map[string]string{
		"omitted":  "",
		"explicit": `,"store":false,"include":["reasoning.encrypted_content"]`,
	} {
		t.Run(name, func(t *testing.T) {
			parsed, err := Decode(strings.NewReader(`{"model":"mock/text","input":"hello"`+controls+`}`), &sequenceIDs{})
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Request.Continuation() != nil {
				t.Fatal("stateless controls fabricated continuation")
			}
		})
	}
}

func TestDecodeMapsStandardReasoningReplayToProviderAffineContinuation(t *testing.T) {
	resolver := &fixedAffinityResolver{providerID: "openai-primary", upstreamModel: "gpt-upstream"}
	body := `{
		"model":"openai/public",
		"store":false,
		"include":["reasoning.encrypted_content"],
		"tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}],
		"input":[
			{"type":"reasoning","id":"rs_upstream","summary":[{"type":"summary_text","text":"looked up docs"}],"encrypted_content":"cipher-marker","status":"completed"},
			{"type":"function_call","id":"fc_upstream","status":"completed","call_id":"call-1","name":"Search","arguments":"{}"},
			{"type":"function_call_output","call_id":"call-1","output":"found"}
		]
	}`
	parsed, err := DecodeWithAffinity(strings.NewReader(body), &sequenceIDs{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.calls)
	}
	items := parsed.Request.Input()
	if len(items) != 3 || items[0].Type() != canonical.ItemType("reasoning_summary") || items[1].Type() != canonical.ItemType("tool_call") || items[2].Type() != canonical.ItemType("tool_result") {
		t.Fatalf("item order/types = %#v", items)
	}
	reasoning, _ := items[0].ReasoningSummary()
	summary := reasoning.Summaries()
	if len(summary) != 1 {
		t.Fatalf("reasoning summary length = %d", len(summary))
	}
	if summary[0].Text() != "looked up docs" {
		t.Fatalf("reasoning summary = %#v", summary)
	}
	continuation := parsed.Request.Continuation()
	if continuation == nil || continuation.Provider() != "openai-primary" || continuation.Model() != "gpt-upstream" || strings.Contains(string(continuation.Bytes()), "openai/public") {
		t.Fatalf("continuation affinity = %#v", continuation)
	}
	if !strings.Contains(string(continuation.Bytes()), "cipher-marker") || !strings.Contains(string(continuation.Bytes()), "rs_upstream") {
		t.Fatal("continuation envelope omitted replay identity or ciphertext")
	}
}

func TestDecodeRejectsInvalidStatelessControls(t *testing.T) {
	for _, test := range []struct {
		name, fields, path string
		code               ErrorCode
	}{
		{"stored", `,"store":true`, "store", CodeUnsupportedField},
		{"null store", `,"store":null`, "store", CodeInvalidField},
		{"empty include", `,"include":[]`, "include", CodeInvalidField},
		{"unsupported include", `,"include":["web_search_call.results"]`, "include[0]", CodeUnsupportedField},
		{"duplicate include", `,"include":["reasoning.encrypted_content","reasoning.encrypted_content"]`, "include[1]", CodeInvalidField},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(`{"model":"mock/text","input":"hello"`+test.fields+`}`), &sequenceIDs{})
			if CodeOf(err) != test.code || PathOf(err) != test.path {
				t.Fatalf("error code/path = %q/%q, want %q/%q", CodeOf(err), PathOf(err), test.code, test.path)
			}
		})
	}
}

func TestDecodeRejectsInvalidReasoningReplaySafely(t *testing.T) {
	const marker = "ultra-private-cipher-marker"
	resolver := &fixedAffinityResolver{providerID: "openai-primary", upstreamModel: "gpt-upstream"}
	for _, test := range []struct {
		name, item, path string
		code             ErrorCode
	}{
		{"missing id", `{"type":"reasoning","summary":[],"encrypted_content":"` + marker + `"}`, "input[0].id", CodeInvalidField},
		{"missing summary", `{"type":"reasoning","id":"rs_1","encrypted_content":"` + marker + `"}`, "input[0].summary", CodeInvalidField},
		{"missing encrypted", `{"type":"reasoning","id":"rs_1","summary":[]}`, "input[0].encrypted_content", CodeInvalidField},
		{"bad status", `{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"` + marker + `","status":"in_progress"}`, "input[0].status", CodeInvalidField},
		{"reasoning content", `{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"` + marker + `","content":[]}`, "input[0].content", CodeUnsupportedField},
		{"bad upstream id", `{"type":"reasoning","id":"bad id","summary":[],"encrypted_content":"` + marker + `"}`, "input[0]", CodeInvalidField},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `{"model":"openai/public","input":[` + test.item + `]}`
			_, err := DecodeWithAffinity(strings.NewReader(body), &sequenceIDs{}, resolver)
			if CodeOf(err) != test.code || PathOf(err) != test.path {
				t.Fatalf("error code/path = %q/%q, want %q/%q", CodeOf(err), PathOf(err), test.code, test.path)
			}
			if err != nil && strings.Contains(err.Error(), marker) {
				t.Fatal("error leaked encrypted content")
			}
		})
	}
}

func TestDecodeReasoningRequiresAffinityResolver(t *testing.T) {
	body := `{"model":"openai/public","input":[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"cipher"}]}`
	_, err := Decode(strings.NewReader(body), &sequenceIDs{})
	if CodeOf(err) != CodeInternal || PathOf(err) != "request.continuation" {
		t.Fatalf("error code/path = %q/%q", CodeOf(err), PathOf(err))
	}
}

func TestDecodeRedactsAffinityResolverFailure(t *testing.T) {
	const marker = "private-resolver-error"
	resolver := &fixedAffinityResolver{err: newError(CodeInternal, marker, nil)}
	body := `{"model":"openai/public","input":[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"private-cipher"}]}`
	_, err := DecodeWithAffinity(strings.NewReader(body), &sequenceIDs{}, resolver)
	if CodeOf(err) != CodeInvalidField || PathOf(err) != "model" {
		t.Fatalf("error code/path = %q/%q", CodeOf(err), PathOf(err))
	}
	if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "private-cipher") {
		t.Fatal("error leaked resolver or continuation details")
	}
}

func TestDecodeWithAffinitySkipsResolverWithoutContinuation(t *testing.T) {
	resolver := &fixedAffinityResolver{err: newError(CodeInternal, "private-marker", nil)}
	parsed, err := DecodeWithAffinity(strings.NewReader(`{"model":"mock/text","input":"hello"}`), &sequenceIDs{}, resolver)
	if err != nil || !parsed.Request.Valid() {
		t.Fatalf("ordinary request failed: %v", err)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver called %d times", resolver.calls)
	}
}

func TestDecodeWithAffinityRejectsTypedNilResolver(t *testing.T) {
	var resolver *fixedAffinityResolver
	_, err := DecodeWithAffinity(strings.NewReader(`{"model":"mock/text","input":"hello"}`), &sequenceIDs{}, resolver)
	if CodeOf(err) != CodeInternal || PathOf(err) != "request.continuation" {
		t.Fatalf("error code/path = %q/%q", CodeOf(err), PathOf(err))
	}
}

func TestDecodePreservesOrderedReasoningEntriesAndRejectsDuplicateUpstreamIDs(t *testing.T) {
	resolver := &fixedAffinityResolver{providerID: "openai-primary", upstreamModel: "gpt-upstream"}
	body := `{"model":"openai/public","input":[{"type":"reasoning","id":"rs_first","summary":[],"encrypted_content":"cipher-first"},{"type":"reasoning","id":"rs_second","summary":[],"encrypted_content":"cipher-second"}]}`
	parsed, err := DecodeWithAffinity(strings.NewReader(body), &sequenceIDs{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(parsed.Request.Continuation().Bytes())
	if first, second := strings.Index(encoded, "rs_first"), strings.Index(encoded, "rs_second"); first < 0 || second <= first {
		t.Fatalf("continuation entry order = %s", encoded)
	}
	duplicate := strings.Replace(body, "rs_second", "rs_first", 1)
	_, err = DecodeWithAffinity(strings.NewReader(duplicate), &sequenceIDs{}, resolver)
	if CodeOf(err) != CodeInvalidField || PathOf(err) != "input" {
		t.Fatalf("duplicate error code/path = %q/%q", CodeOf(err), PathOf(err))
	}
}

func TestDecodeMapsEmptyReasoningSummaryWithContinuation(t *testing.T) {
	resolver := &fixedAffinityResolver{providerID: "openai-primary", upstreamModel: "gpt-upstream"}
	body := `{"model":"openai/public","input":[{"type":"reasoning","id":"rs_empty","summary":[],"encrypted_content":"cipher"}]}`
	parsed, err := DecodeWithAffinity(strings.NewReader(body), &sequenceIDs{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	reasoning, ok := parsed.Request.Input()[0].ReasoningSummary()
	if !ok || len(reasoning.Summaries()) != 0 || parsed.Request.Continuation() == nil {
		t.Fatal("empty continuation-backed reasoning was not preserved")
	}
}
