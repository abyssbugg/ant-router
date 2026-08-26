package canonical

import "testing"

func TestResponse(t *testing.T) {
	message, _ := NewMessage(testItemID(t, 1), Role("assistant"), []ContentBlock{testTextBlock(t, "hello")})
	messageItem, _ := NewMessageItem(message)
	usage, _ := NewUsage(UsageParams{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	items := []Item{messageItem}
	response, err := NewResponse(ResponseParams{ID: testResponseID(t, 1), Model: "model", Items: items, FinishReason: FinishReason("stop"), Usage: usage})
	if err != nil || !response.Valid() || response.FinishReason() != FinishReason("stop") || response.Usage().TotalTokens() != 15 {
		t.Fatalf("NewResponse() = %#v, %v", response, err)
	}
	items[0] = Item{}
	copyItems := response.Items()
	copyItems[0] = Item{}
	if !response.Items()[0].Valid() {
		t.Error("response items were mutated")
	}
	empty, err := NewResponse(ResponseParams{ID: testResponseID(t, 2), Model: "model", FinishReason: FinishReason("content_filter"), Usage: Usage{}})
	if err != nil || !empty.Valid() || len(empty.Items()) != 0 {
		t.Errorf("empty response = %#v, %v", empty, err)
	}
}

func TestResponseToolCallFinish(t *testing.T) {
	call, _ := NewToolCall(testItemID(t, 2), testCallID(t, 2), "Search", testJSONObject(t, `{}`))
	callItem, _ := NewToolCallItem(call)
	response, err := NewResponse(ResponseParams{ID: testResponseID(t, 2), Model: "model", Items: []Item{callItem}, FinishReason: FinishReason("tool_call"), Usage: Usage{}})
	if err != nil || !response.Valid() {
		t.Fatalf("tool response = %#v, %v", response, err)
	}
}

func TestResponseRejectsInvalid(t *testing.T) {
	message, _ := NewMessage(testItemID(t, 1), Role("assistant"), []ContentBlock{testTextBlock(t, "hello")})
	messageItem, _ := NewMessageItem(message)
	call, _ := NewToolCall(testItemID(t, 2), testCallID(t, 2), "Search", testJSONObject(t, `{}`))
	callItem, _ := NewToolCallItem(call)
	result, _ := NewToolResult(testItemID(t, 3), testCallID(t, 2), []ContentBlock{testTextBlock(t, "result")}, false)
	resultItem, _ := NewToolResultItem(result)
	invalidUsage := Usage{}
	invalidUsage.totalTokens = 1
	cases := []ResponseParams{
		{ID: "", Model: "model", FinishReason: FinishReason("stop")},
		{ID: testResponseID(t, 1), Model: "", FinishReason: FinishReason("stop")},
		{ID: testResponseID(t, 1), Model: "model", Items: []Item{messageItem, messageItem}, FinishReason: FinishReason("stop")},
		{ID: testResponseID(t, 1), Model: "model", Items: []Item{resultItem}, FinishReason: FinishReason("stop")},
		{ID: testResponseID(t, 1), Model: "model", Items: []Item{callItem, callItem}, FinishReason: FinishReason("tool_call")},
		{ID: testResponseID(t, 1), Model: "model", Items: []Item{callItem}, FinishReason: FinishReason("stop")},
		{ID: testResponseID(t, 1), Model: "model", Items: []Item{messageItem}, FinishReason: FinishReason("tool_call")},
		{ID: testResponseID(t, 1), Model: "model", FinishReason: FinishReason("cancelled")},
		{ID: testResponseID(t, 1), Model: "model", FinishReason: FinishReason("stop"), Usage: invalidUsage},
	}
	for index, params := range cases {
		if _, err := NewResponse(params); err == nil {
			t.Errorf("case %d accepted: %#v", index, params)
		}
	}
}
