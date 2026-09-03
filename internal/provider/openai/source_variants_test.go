package openai

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	openaicontinuation "github.com/abyssbugg/ant-router/internal/openai/continuation"
	"github.com/abyssbugg/ant-router/internal/streaming"
	sdkresponses "github.com/openai/openai-go/v3/responses"
)

func runSDKEvents(t *testing.T, events []sdkresponses.ResponseStreamEventUnion, format canonical.OutputFormat) (canonical.Response, streaming.Transition) {
	t.Helper()
	source, err := newEventSource(&fakeSDKStream{events: events}, "openai/public", format, &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	var terminal streaming.Transition
	for {
		event, recvErr := source.Recv(context.Background())
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			t.Fatalf("Recv: %v", recvErr)
		}
		terminal, err = machine.Accept(event)
		if err != nil {
			t.Fatalf("Accept(%s): %v", event.Type(), err)
		}
	}
	response, _ := terminal.Response()
	return response, terminal
}

func toolSDKEvents(t *testing.T) []sdkresponses.ResponseStreamEventUnion {
	t.Helper()
	return []sdkresponses.ResponseStreamEventUnion{
		sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"r","model":"gpt","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"fc_upstream","type":"function_call","call_id":"call_up","name":"Search","arguments":"","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.function_call_arguments.delta","sequence_number":2,"output_index":0,"item_id":"fc_upstream","delta":"{\"query\":"}`),
		sdkStreamEvent(t, `{"type":"response.function_call_arguments.delta","sequence_number":3,"output_index":0,"item_id":"fc_upstream","delta":"\"docs\"}"}`),
		sdkStreamEvent(t, `{"type":"response.function_call_arguments.done","sequence_number":4,"output_index":0,"item_id":"fc_upstream","name":"Search","arguments":"{\"query\":\"docs\"}"}`),
		sdkStreamEvent(t, `{"type":"response.output_item.done","sequence_number":5,"output_index":0,"item":{"id":"fc_upstream","type":"function_call","call_id":"call_up","name":"Search","arguments":"{\"query\":\"docs\"}","status":"completed"}}`),
		sdkStreamEvent(t, `{"type":"response.completed","sequence_number":6,"response":{"id":"r","model":"gpt","status":"completed","usage":{"input_tokens":2,"input_tokens_details":{"cached_tokens":0},"output_tokens":3,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":5}}}`),
	}
}

func TestEventSourceMapsToolCallStream(t *testing.T) {
	response, _ := runSDKEvents(t, toolSDKEvents(t), canonical.OutputFormat("text"))
	if response.FinishReason() != canonical.FinishReason("tool_call") || len(response.Items()) != 1 {
		t.Fatalf("finish/items = %q/%d", response.FinishReason(), len(response.Items()))
	}
	call, ok := response.Items()[0].ToolCall()
	if !ok || call.Name() != "Search" || string(call.Arguments().Bytes()) != `{"query":"docs"}` {
		t.Fatalf("tool call = %#v, ok=%v", call, ok)
	}
	continuation, ok := response.Continuation()
	if !ok {
		t.Fatal("tool response omitted continuation")
	}
	entries, err := openaicontinuation.DecodeContinuation(continuation, continuation.Provider(), continuation.Model(), []canonical.ItemID{response.Items()[0].ID()})
	if err != nil || len(entries) != 1 || !entries[0].IsFunctionCall() || entries[0].CanonicalItemID() != response.Items()[0].ID() || entries[0].CanonicalCallID() != call.CallID() || entries[0].UpstreamItemID() != "fc_upstream" || entries[0].UpstreamCallID() != "call_up" {
		t.Fatalf("tool continuation = %#v, error = %v", entries, err)
	}
}

func reasoningSDKEvents(t *testing.T) []sdkresponses.ResponseStreamEventUnion {
	t.Helper()
	return []sdkresponses.ResponseStreamEventUnion{
		sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"r","model":"gpt","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"rs_test","type":"reasoning","summary":[],"status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_part.added","sequence_number":2,"output_index":0,"item_id":"rs_test","summary_index":0,"part":{"type":"summary_text","text":""}}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_text.delta","sequence_number":3,"output_index":0,"item_id":"rs_test","summary_index":0,"delta":"checked"}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_text.done","sequence_number":4,"output_index":0,"item_id":"rs_test","summary_index":0,"text":"checked"}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_part.done","sequence_number":5,"output_index":0,"item_id":"rs_test","summary_index":0,"part":{"type":"summary_text","text":"checked"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.done","sequence_number":6,"output_index":0,"item":{"id":"rs_test","type":"reasoning","summary":[{"type":"summary_text","text":"checked"}],"encrypted_content":"cipher-test","status":"completed"}}`),
		sdkStreamEvent(t, `{"type":"response.completed","sequence_number":7,"response":{"id":"r","model":"gpt","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":3}}}`),
	}
}

func TestEventSourceMapsReasoningSummaryStream(t *testing.T) {
	response, _ := runSDKEvents(t, reasoningSDKEvents(t), canonical.OutputFormat("text"))
	summary, ok := response.Items()[0].ReasoningSummary()
	if !ok || len(summary.Summaries()) != 1 || summary.Summaries()[0].Text() != "checked" || response.Usage().ReasoningTokens() != 2 {
		t.Fatalf("reasoning response = %#v", response)
	}
}

func TestEventSourceMapsStructuredJSONStream(t *testing.T) {
	events := textSDKEvents(t)
	// Replace the text payloads while retaining the already-valid event structure.
	events[4] = sdkStreamEvent(t, `{"type":"response.output_text.delta","sequence_number":4,"output_index":0,"content_index":0,"item_id":"msg_upstream","delta":"{\"ok\":true}"}`)
	events[5] = sdkStreamEvent(t, `{"type":"response.output_text.done","sequence_number":5,"output_index":0,"content_index":0,"item_id":"msg_upstream","text":"{\"ok\":true}"}`)
	events[6] = sdkStreamEvent(t, `{"type":"response.content_part.done","sequence_number":6,"output_index":0,"content_index":0,"item_id":"msg_upstream","part":{"type":"output_text","text":"{\"ok\":true}"}}`)
	events[7] = sdkStreamEvent(t, `{"type":"response.output_item.done","sequence_number":7,"output_index":0,"item":{"id":"msg_upstream","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"{\"ok\":true}"}]}}`)
	response, _ := runSDKEvents(t, events, canonical.OutputFormat("json_schema"))
	content, _ := response.Items()[0].Message()
	value, ok := content.Content()[0].JSON()
	if !ok || string(value.Value().Bytes()) != `{"ok":true}` {
		t.Fatalf("JSON content = %#v, ok=%v", value, ok)
	}
}

func TestEventSourceMapsIncompleteAndFailedTerminals(t *testing.T) {
	for _, test := range []struct {
		name, terminal, wantFinish string
		wantFailure                bool
	}{
		{"length", `{"type":"response.incomplete","sequence_number":1,"response":{"id":"r","model":"gpt","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":1,"output_tokens":0,"total_tokens":1}}}`, "length", false},
		{"filter", `{"type":"response.incomplete","sequence_number":1,"response":{"id":"r","model":"gpt","status":"incomplete","incomplete_details":{"reason":"content_filter"},"usage":{"input_tokens":1,"output_tokens":0,"total_tokens":1}}}`, "content_filter", false},
		{"failed", `{"type":"response.failed","sequence_number":1,"response":{"id":"r","model":"gpt","status":"failed","error":{"code":"server_error","message":"private failure canary"}}}`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := []sdkresponses.ResponseStreamEventUnion{
				sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"r","model":"gpt","status":"in_progress"}}`),
				sdkStreamEvent(t, test.terminal),
			}
			_, terminal := runSDKEvents(t, events, canonical.OutputFormat("text"))
			if test.wantFailure {
				failure, ok := terminal.Failure()
				if !ok || failure.Code() != canonical.FailureCode("upstream_protocol_error") || failure.Message() == "private failure canary" {
					t.Fatalf("failure = %#v, ok=%v", failure, ok)
				}
				return
			}
			response, ok := terminal.Response()
			if !ok || string(response.FinishReason()) != test.wantFinish {
				t.Fatalf("response finish = %#v/%v", response, ok)
			}
		})
	}
}

func TestEventSourceRejectsStatusEventMismatches(t *testing.T) {
	for _, event := range []string{
		`{"type":"response.created","sequence_number":0,"response":{"id":"r","model":"m","status":"failed"}}`,
		`{"type":"response.queued","sequence_number":0,"response":{"id":"r","model":"m","status":"completed"}}`,
		`{"type":"response.in_progress","sequence_number":0,"response":{"id":"r","model":"m","status":"completed"}}`,
	} {
		source, err := newEventSource(&fakeSDKStream{events: []sdkresponses.ResponseStreamEventUnion{sdkStreamEvent(t, event)}}, "openai/public", canonical.OutputFormat("text"), &sourceIDs{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = source.Recv(context.Background()); execution.CodeOf(err) != execution.CodeInternal {
			t.Fatalf("status mismatch error = %v", err)
		}
	}
}

func TestEventSourceRejectsDoneAndTerminalIntegrityMismatches(t *testing.T) {
	for _, test := range []struct {
		name  string
		index int
		event string
	}{
		{"content part", 6, `{"type":"response.content_part.done","sequence_number":6,"output_index":0,"content_index":0,"item_id":"msg_upstream","part":{"type":"output_text","text":"different"}}`},
		{"item done", 7, `{"type":"response.output_item.done","sequence_number":7,"output_index":0,"item":{"id":"msg_upstream","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"different"}]}}`},
		{"terminal status", 8, `{"type":"response.completed","sequence_number":8,"response":{"id":"resp_upstream","model":"gpt-upstream","status":"failed","usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := textSDKEvents(t)
			events[test.index] = sdkStreamEvent(t, test.event)
			source, err := newEventSource(&fakeSDKStream{events: events}, "openai/public", canonical.OutputFormat("text"), &sourceIDs{})
			if err != nil {
				t.Fatal(err)
			}
			for err == nil {
				_, err = source.Recv(context.Background())
			}
			if execution.CodeOf(err) != execution.CodeInternal {
				t.Fatalf("error = %v, code=%q", err, execution.CodeOf(err))
			}
		})
	}
}

func TestEventSourceReturnsSafeErrorEvent(t *testing.T) {
	source, err := newEventSource(&fakeSDKStream{events: []sdkresponses.ResponseStreamEventUnion{
		sdkStreamEvent(t, `{"type":"error","sequence_number":0,"code":"server_error","message":"private error canary","param":"private_param"}`),
	}}, "openai/public", canonical.OutputFormat("text"), &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Recv(context.Background())
	if execution.CodeOf(err) != execution.CodeInternal || containsText(err.Error(), "private error canary") || containsText(err.Error(), "private_param") {
		t.Fatalf("error event = %v", err)
	}
}
