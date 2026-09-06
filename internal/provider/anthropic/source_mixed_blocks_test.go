package anthropic

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

func TestEventSourceReconstructsTextToolTextBlocks(t *testing.T) {
	events := []sdk.MessageStreamEventUnion{
		messageStart(t),
		sdkStreamEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sdkStreamEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"before"}}`),
		sdkStreamEvent(t, `{"type":"content_block_stop","index":0}`),
		sdkStreamEvent(t, `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_upstream","name":"Search","input":{}}}`),
		sdkStreamEvent(t, `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"docs\"}"}}`),
		sdkStreamEvent(t, `{"type":"content_block_stop","index":1}`),
		sdkStreamEvent(t, `{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`),
		sdkStreamEvent(t, `{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"after"}}`),
		sdkStreamEvent(t, `{"type":"content_block_stop","index":2}`),
	}
	events = append(events, terminalEvents(t, "tool_use", 6)...)

	response, _ := drainAnthropic(t, events, canonical.OutputFormat("text"))
	items := response.Items()
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	message, ok := items[0].Message()
	if !ok {
		t.Fatal("first item is not a message")
	}
	blocks := message.Content()
	if len(blocks) != 2 {
		t.Fatalf("message blocks = %d, want 2", len(blocks))
	}
	first, ok := blocks[0].Text()
	if !ok || first.Text() != "before" {
		t.Fatalf("first block = %#v", blocks[0])
	}
	second, ok := blocks[1].Text()
	if !ok || second.Text() != "after" {
		t.Fatalf("second block = %#v", blocks[1])
	}
	call, ok := items[1].ToolCall()
	if !ok || call.Name() != "Search" || string(call.Arguments().Bytes()) != `{"q":"docs"}` {
		t.Fatalf("tool item = %#v", items[1])
	}
}
