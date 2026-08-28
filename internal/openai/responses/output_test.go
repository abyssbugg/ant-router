package responses

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func TestEncodeResponseMapsCanonicalVariants(t *testing.T) {
	encoder, err := NewEncoder(time.Unix(1_700_000_000, 999))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"text", "json", "tool", "reasoning"} {
		t.Run(kind, func(t *testing.T) {
			response := outputResponse(t, kind)
			encoded, err := encoder.EncodeResponse(response)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			if wire["object"] != "response" || wire["status"] != "completed" || wire["model"] != "mock/"+kind || wire["created_at"] != float64(1_700_000_000) {
				t.Fatalf("response envelope = %#v", wire)
			}
			output := wire["output"].([]any)[0].(map[string]any)
			switch kind {
			case "text":
				assertWireText(t, output, "hello")
			case "json":
				assertWireText(t, output, `{"ok":true}`)
			case "tool":
				if output["type"] != "function_call" || output["name"] != "Search" || output["arguments"] != `{"q":"x"}` {
					t.Fatalf("tool output = %#v", output)
				}
			case "reasoning":
				summary := output["summary"].([]any)[0].(map[string]any)
				if output["type"] != "reasoning" || summary["type"] != "summary_text" || summary["text"] != "summary" {
					t.Fatalf("reasoning output = %#v", output)
				}
			}
			usage := wire["usage"].(map[string]any)
			if usage["input_tokens"] != float64(2) || usage["output_tokens"] != float64(3) || usage["total_tokens"] != float64(5) {
				t.Fatalf("usage = %#v", usage)
			}
		})
	}
}

func TestEncoderRejectsInvalidTimestampAndResponse(t *testing.T) {
	if _, err := NewEncoder(time.Unix(-1, 0)); err == nil {
		t.Fatal("negative timestamp accepted")
	}
	encoder, _ := NewEncoder(time.Unix(1, 0))
	if _, err := encoder.EncodeResponse(canonical.Response{}); CodeOf(err) != CodeInternal || PathOf(err) != "response" {
		t.Fatalf("invalid response error = %v", err)
	}
}

func assertWireText(t *testing.T, output map[string]any, want string) {
	t.Helper()
	content := output["content"].([]any)[0].(map[string]any)
	if output["type"] != "message" || output["role"] != "assistant" || content["type"] != "output_text" || content["text"] != want {
		t.Fatalf("text output = %#v", output)
	}
}

func outputResponse(t *testing.T, kind string) canonical.Response {
	t.Helper()
	ids := &outputIDs{}
	itemID, _ := ids.ItemID()
	var item canonical.Item
	finish := canonical.FinishReason("stop")
	switch kind {
	case "text":
		text, _ := canonical.NewTextContent("hello")
		block, _ := canonical.NewTextBlock(text)
		message, _ := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
		item, _ = canonical.NewMessageItem(message)
	case "json":
		value, _ := canonicaljson.Parse([]byte(`{"ok":true}`), canonicaljson.DefaultLimits())
		content, _ := canonical.NewJSONContent(value)
		block, _ := canonical.NewJSONBlock(content)
		message, _ := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
		item, _ = canonical.NewMessageItem(message)
	case "tool":
		value, _ := canonicaljson.Parse([]byte(`{"q":"x"}`), canonicaljson.DefaultLimits())
		callID, _ := ids.ToolCallID()
		call, _ := canonical.NewToolCall(itemID, callID, "Search", value)
		item, _ = canonical.NewToolCallItem(call)
		finish = canonical.FinishReason("tool_call")
	case "reasoning":
		text, _ := canonical.NewTextContent("summary")
		reasoning, _ := canonical.NewReasoningSummary(itemID, []canonical.TextContent{text})
		item, _ = canonical.NewReasoningSummaryItem(reasoning)
	}
	usage, _ := canonical.NewUsage(canonical.UsageParams{InputTokens: 2, OutputTokens: 3, ReasoningTokens: func() int64 {
		if kind == "reasoning" {
			return 1
		}
		return 0
	}(), TotalTokens: 5})
	responseID, _ := ids.ResponseID()
	response, err := canonical.NewResponse(canonical.ResponseParams{ID: responseID, Model: "mock/" + kind, Items: []canonical.Item{item}, FinishReason: finish, Usage: usage})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

type outputIDs struct{ next byte }

func (i *outputIDs) reader() *bytes.Reader {
	i.next++
	return bytes.NewReader(bytes.Repeat([]byte{i.next}, 16))
}
func (i *outputIDs) EventID() (canonical.EventID, error) {
	return canonical.GenerateEventID(i.reader())
}
func (i *outputIDs) ResponseID() (canonical.ResponseID, error) {
	return canonical.GenerateResponseID(i.reader())
}
func (i *outputIDs) ItemID() (canonical.ItemID, error) { return canonical.GenerateItemID(i.reader()) }
func (i *outputIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(i.reader())
}
