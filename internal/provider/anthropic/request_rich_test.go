package anthropic

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func TestMapRequestPreservesSystemAndAssistantJSON(t *testing.T) {
	base := decodeRequest(t, `{"model":"anthropic/public","input":"hello"}`)
	system := jsonMessage(t, &requestIDs{next: 20}, canonical.Role("system"), `{"policy":"safe"}`)
	assistant := jsonMessage(t, &requestIDs{next: 40}, canonical.Role("assistant"), `{"ok":true}`)
	request := replaceInput(t, base, []canonical.Item{system, base.Input()[0], assistant})
	params, err := mapRequest(request, "claude-upstream", 1024, 4096)
	if err != nil {
		t.Fatalf("mapRequest: %v", err)
	}
	object := requestObject(t, params)
	if asObject(t, asArray(t, object["system"], "system")[0], "system[0]")["text"] != `{"policy":"safe"}` {
		t.Fatalf("system = %#v", object["system"])
	}
	messages := asArray(t, object["messages"], "messages")
	assistantContent := asArray(t, asObject(t, messages[1], "assistant")["content"], "assistant.content")
	if asObject(t, assistantContent[0], "assistant.content[0]")["text"] != `{"ok":true}` {
		t.Fatalf("assistant = %#v", assistantContent)
	}
}

func TestMapRequestPreservesRichErrorToolResult(t *testing.T) {
	base := decodeRequest(t, `{"model":"anthropic/public","input":[{"type":"function_call","call_id":"external","name":"Search","arguments":"{}"},{"type":"function_call_output","call_id":"external","output":"old"}],"tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}]}`)
	items := base.Input()
	call, _ := items[0].ToolCall()
	oldResult, _ := items[1].ToolResult()
	image, err := canonical.NewInlineImage([]byte("image"), "image/png", canonical.ImageDetailAuto, 1, 1)
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	imageBlock, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatalf("image block: %v", err)
	}
	jsonBlock := jsonContentBlock(t, `{"error":"not found"}`)
	result, err := canonical.NewToolResult(oldResult.ID(), call.CallID(), []canonical.ContentBlock{jsonBlock, imageBlock}, true)
	if err != nil {
		t.Fatalf("tool result: %v", err)
	}
	resultItem, err := canonical.NewToolResultItem(result)
	if err != nil {
		t.Fatalf("tool result item: %v", err)
	}
	request := replaceInput(t, base, []canonical.Item{items[0], resultItem})
	params, err := mapRequest(request, "claude-upstream", 1024, 4096)
	if err != nil {
		t.Fatalf("mapRequest: %v", err)
	}
	messages := asArray(t, requestObject(t, params)["messages"], "messages")
	content := asArray(t, asObject(t, messages[1], "result turn")["content"], "result content")
	resultObject := asObject(t, content[0], "tool result")
	if resultObject["is_error"] != true || len(asArray(t, resultObject["content"], "tool result content")) != 2 {
		t.Fatalf("tool result = %#v", resultObject)
	}
}

func jsonMessage(t *testing.T, ids *requestIDs, role canonical.Role, raw string) canonical.Item {
	t.Helper()
	block := jsonContentBlock(t, raw)
	id, _ := ids.ItemID()
	message, err := canonical.NewMessage(id, role, []canonical.ContentBlock{block})
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		t.Fatalf("message item: %v", err)
	}
	return item
}

func jsonContentBlock(t *testing.T, raw string) canonical.ContentBlock {
	t.Helper()
	value, err := canonicaljson.Parse([]byte(raw), canonicaljson.DefaultLimits())
	if err != nil {
		t.Fatalf("canonical JSON: %v", err)
	}
	content, err := canonical.NewJSONContent(value)
	if err != nil {
		t.Fatalf("JSON content: %v", err)
	}
	block, err := canonical.NewJSONBlock(content)
	if err != nil {
		t.Fatalf("JSON block: %v", err)
	}
	return block
}

func replaceInput(t *testing.T, base canonical.Request, input []canonical.Item) canonical.Request {
	t.Helper()
	request, err := canonical.NewRequest(canonical.RequestParams{ID: base.ID(), Model: base.Model(), Input: input, Tools: base.Tools(), ToolChoice: base.ToolChoice(), Output: base.Output(), Sampling: base.Sampling(), Continuation: base.Continuation(), ReasoningSummary: base.ReasoningSummary()})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return request
}
