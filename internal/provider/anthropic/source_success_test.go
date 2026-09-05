package anthropic

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

func drainAnthropic(t *testing.T, events []sdk.MessageStreamEventUnion, format canonical.OutputFormat) (canonical.Response, []canonical.EventType) {
	t.Helper()
	source, err := newEventSource(&fakeSDKStream{events: events}, "anthropic/public", format, &sourceIDs{})
	if err != nil {
		t.Fatalf("newEventSource: %v", err)
	}
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		t.Fatalf("NewMachine: %v", err)
	}
	var types []canonical.EventType
	for {
		event, recvErr := source.Recv(context.Background())
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			t.Fatalf("Recv: %v", recvErr)
		}
		types = append(types, event.Type())
		transition, acceptErr := machine.Accept(event)
		if acceptErr != nil {
			t.Fatalf("Accept(%s): %v", event.Type(), acceptErr)
		}
		if response, ok := transition.Response(); ok {
			return response, types
		}
	}
	t.Fatal("stream ended without response")
	return canonical.Response{}, nil
}

func messageStart(t *testing.T) sdk.MessageStreamEventUnion {
	t.Helper()
	return sdkStreamEvent(t, `{"type":"message_start","message":{"id":"msg_upstream","type":"message","role":"assistant","content":[],"model":"claude-test","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":0,"cache_creation_input_tokens":2,"cache_read_input_tokens":3}}}`)
}

func terminalEvents(t *testing.T, reason string, output int) []sdk.MessageStreamEventUnion {
	t.Helper()
	return []sdk.MessageStreamEventUnion{
		sdkStreamEvent(t, `{"type":"message_delta","delta":{"stop_reason":"`+reason+`","stop_sequence":null},"usage":{"output_tokens":`+jsonIndex(output)+`}}`),
		sdkStreamEvent(t, `{"type":"message_stop"}`),
	}
}

func TestEventSourceReconstructsStructuredJSON(t *testing.T) {
	events := []sdk.MessageStreamEventUnion{
		messageStart(t),
		sdkStreamEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sdkStreamEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"{\"ok\":true}"}}`),
		sdkStreamEvent(t, `{"type":"content_block_stop","index":0}`),
	}
	events = append(events, terminalEvents(t, "end_turn", 4)...)
	response, _ := drainAnthropic(t, events, canonical.OutputFormat("json_schema"))
	items := response.Items()
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	message, ok := items[0].Message()
	if !ok {
		t.Fatal("missing message")
	}
	blocks := message.Content()
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	value, ok := blocks[0].JSON()
	if !ok || string(value.Value().Bytes()) != `{"ok":true}` {
		t.Fatalf("JSON block = %#v", blocks[0])
	}
	if response.Usage().InputTokens() != 10 || response.Usage().CacheReadTokens() != 3 || response.Usage().CacheWriteTokens() != 2 || response.Usage().OutputTokens() != 4 {
		t.Fatalf("usage = %#v", response.Usage())
	}
}

func TestEventSourceReconstructsToolCall(t *testing.T) {
	events := []sdk.MessageStreamEventUnion{
		messageStart(t),
		sdkStreamEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_upstream","name":"Search","input":{}}}`),
		sdkStreamEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"docs\"}"}}`),
		sdkStreamEvent(t, `{"type":"content_block_stop","index":0}`),
	}
	events = append(events, terminalEvents(t, "tool_use", 2)...)
	response, types := drainAnthropic(t, events, canonical.OutputFormat("text"))
	items := response.Items()
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	call, ok := items[0].ToolCall()
	if !ok || call.Name() != "Search" || string(call.Arguments().Bytes()) != `{"q":"docs"}` {
		t.Fatalf("tool call = %#v", items[0])
	}
	if response.FinishReason() != canonical.FinishReason("tool_call") {
		t.Fatalf("finish = %s", response.FinishReason())
	}
	if len(types) < 3 || types[2] != canonical.EventType("item_started") {
		t.Fatalf("types = %v", types)
	}
}

func TestMapStopReason(t *testing.T) {
	tests := map[sdk.StopReason]canonical.FinishReason{"end_turn": "stop", "stop_sequence": "stop", "max_tokens": "length", "model_context_window_exceeded": "length", "tool_use": "tool_call", "refusal": "refusal"}
	for input, expected := range tests {
		got, err := mapStopReason(input)
		if err != nil || got != expected {
			t.Fatalf("mapStopReason(%q) = %q, %v", input, got, err)
		}
	}
	if _, err := mapStopReason("pause_turn"); err == nil {
		t.Fatal("pause_turn accepted")
	}
}
