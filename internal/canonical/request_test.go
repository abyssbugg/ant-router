package canonical

import (
	"strings"
	"testing"
)

func TestCanonicalRequestAndCapabilities(t *testing.T) {
	message, _ := NewMessage(testItemID(t, 1), Role("user"), []ContentBlock{testTextBlock(t, "hello")})
	messageItem, _ := NewMessageItem(message)
	tool := mustTool(t, "Search")
	call, _ := NewToolCall(testItemID(t, 2), testCallID(t, 2), "Search", testJSONObject(t, `{"query":"hello"}`))
	callItem, _ := NewToolCallItem(call)
	result, _ := NewToolResult(testItemID(t, 3), testCallID(t, 2), []ContentBlock{testTextBlock(t, "result")}, false)
	resultItem, _ := NewToolResultItem(result)
	schema := testJSONObject(t, `{"type":"object"}`)
	output, _ := NewOutputConstraints(OutputParams{Format: OutputFormat("json_schema"), SchemaName: "Answer", Schema: schema, ParallelToolCalls: true})
	sampling, _ := NewSampling(SamplingParams{Temperature: pointer(0.5), Seed: pointer[int64](42)})
	continuation, _ := NewContinuation("openai", "model-a", []byte{1, 2, 3})
	choice, _ := NewToolChoice(ToolChoiceKind("specific"), "Search")
	input := []Item{messageItem, callItem, resultItem}
	tools := []ToolDefinition{tool}
	request, err := NewRequest(RequestParams{ID: testRequestID(t, 9), Model: "model-a", Input: input, Tools: tools, ToolChoice: choice, Output: output, Sampling: sampling, Continuation: &continuation})
	if err != nil || !request.Valid() {
		t.Fatalf("NewRequest() failed: %v", err)
	}
	input[0] = Item{}
	tools[0] = ToolDefinition{}
	if !request.Input()[0].Valid() || !request.Tools()[0].Valid() {
		t.Error("request slices were mutated")
	}
	for _, capability := range []Capability{CapabilityText, CapabilityTools, CapabilityStructuredOutput, CapabilityParallelTools, CapabilityContinuation} {
		if !request.Capabilities().Has(capability) {
			t.Errorf("missing capability %q", capability)
		}
	}
	without := request.WithoutContinuation()
	if without.Continuation() != nil || !without.Valid() || without.ID() != request.ID() {
		t.Errorf("WithoutContinuation() = %#v", without)
	}
}

func TestRequestImageAndReasoningCapabilities(t *testing.T) {
	image, _ := NewInlineImage([]byte{1}, "image/png", ImageDetailAuto, 0, 0)
	imageBlock, _ := NewImageBlock(image)
	message, _ := NewMessage(testItemID(t, 1), Role("user"), []ContentBlock{imageBlock})
	messageItem, _ := NewMessageItem(message)
	text, _ := NewTextContent("summary")
	reasoning, _ := NewReasoningSummary(testItemID(t, 2), []TextContent{text})
	reasoningItem, _ := NewReasoningSummaryItem(reasoning)
	request, err := NewRequest(RequestParams{ID: testRequestID(t, 1), Model: "model", Input: []Item{messageItem, reasoningItem}})
	if err != nil {
		t.Fatalf("NewRequest() failed: %v", err)
	}
	if !request.Capabilities().Has(CapabilityImageInput) || !request.Capabilities().Has(CapabilityReasoningSummary) {
		t.Errorf("capabilities = %v", request.Capabilities().Values())
	}
}

func TestRequestCrossInvariantRejections(t *testing.T) {
	message, _ := NewMessage(testItemID(t, 1), Role("user"), []ContentBlock{testTextBlock(t, "hello")})
	messageItem, _ := NewMessageItem(message)
	tool := mustTool(t, "Search")
	call, _ := NewToolCall(testItemID(t, 2), testCallID(t, 2), "Search", testJSONObject(t, `{}`))
	callItem, _ := NewToolCallItem(call)
	result, _ := NewToolResult(testItemID(t, 3), testCallID(t, 2), []ContentBlock{testTextBlock(t, "result")}, false)
	resultItem, _ := NewToolResultItem(result)
	auto, _ := NewToolChoice(ToolChoiceKind("auto"), "")

	cases := []RequestParams{
		{ID: "", Model: "model", Input: []Item{messageItem}},
		{ID: testRequestID(t, 1), Model: "", Input: []Item{messageItem}},
		{ID: testRequestID(t, 1), Model: string([]byte{0xff}), Input: []Item{messageItem}},
		{ID: testRequestID(t, 1), Model: strings.Repeat("m", MaxModelNameBytes+1), Input: []Item{messageItem}},
		{ID: testRequestID(t, 1), Model: "model", Input: nil},
		{ID: testRequestID(t, 1), Model: "model", Input: []Item{messageItem, messageItem}},
		{ID: testRequestID(t, 1), Model: "model", Input: []Item{resultItem, callItem}, Tools: []ToolDefinition{tool}, ToolChoice: auto},
		{ID: testRequestID(t, 1), Model: "model", Input: []Item{callItem, resultItem, callItem}, Tools: []ToolDefinition{tool}, ToolChoice: auto},
		{ID: testRequestID(t, 1), Model: "model", Input: []Item{callItem}, Tools: nil, ToolChoice: auto},
		{ID: testRequestID(t, 1), Model: "model", Input: []Item{messageItem}, Tools: []ToolDefinition{tool, tool}, ToolChoice: auto},
	}
	for index, params := range cases {
		if _, err := NewRequest(params); err == nil {
			t.Errorf("case %d accepted: %#v", index, params)
		}
	}

	unknownChoice, _ := NewToolChoice(ToolChoiceKind("specific"), "Other")
	if _, err := NewRequest(RequestParams{ID: testRequestID(t, 1), Model: "model", Input: []Item{messageItem}, Tools: []ToolDefinition{tool}, ToolChoice: unknownChoice}); err == nil {
		t.Error("unknown specific tool choice accepted")
	}
	continuation, _ := NewContinuation("openai-primary", "gpt-upstream", []byte{1})
	request, err := NewRequest(RequestParams{ID: testRequestID(t, 1), Model: "openai/public", Input: []Item{messageItem}, Continuation: &continuation})
	if err != nil || !request.Valid() {
		t.Fatalf("provider-affine continuation rejected before registry resolution: %v", err)
	}
}

func TestRequestItemAndToolLimits(t *testing.T) {
	message, _ := NewMessage(testItemID(t, 1), Role("user"), []ContentBlock{testTextBlock(t, "hello")})
	item, _ := NewMessageItem(message)
	items := make([]Item, MaxInputItems+1)
	for index := range items {
		items[index] = item
	}
	if _, err := NewRequest(RequestParams{ID: testRequestID(t, 1), Model: "model", Input: items}); codeOfCanonical(err) != CodeLimitExceeded {
		t.Errorf("item limit code = %q", codeOfCanonical(err))
	}
	tools := make([]ToolDefinition, MaxTools+1)
	for index := range tools {
		tools[index] = mustTool(t, string(rune('A'+index%26))+strings.Repeat("x", index/26))
	}
	if _, err := NewRequest(RequestParams{ID: testRequestID(t, 1), Model: "model", Input: []Item{item}, Tools: tools}); codeOfCanonical(err) != CodeLimitExceeded {
		t.Errorf("tool limit code = %q", codeOfCanonical(err))
	}
}

func mustTool(t *testing.T, name string) ToolDefinition {
	t.Helper()
	tool, err := NewToolDefinition(name, "", testJSONObject(t, `{}`))
	if err != nil {
		t.Fatalf("NewToolDefinition(%q) failed: %v", name, err)
	}
	return tool
}
