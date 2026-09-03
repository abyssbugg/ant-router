package openai

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	openaicontinuation "github.com/abyssbugg/ant-router/internal/openai/continuation"
	sdkresponses "github.com/openai/openai-go/v3/responses"
)

func reasoningContinuationEvents(t *testing.T, encrypted string) []sdkresponses.ResponseStreamEventUnion {
	t.Helper()
	return []sdkresponses.ResponseStreamEventUnion{
		sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"r","model":"gpt","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"rs_upstream","type":"reasoning","summary":[],"status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_part.added","sequence_number":2,"output_index":0,"item_id":"rs_upstream","summary_index":0,"part":{"type":"summary_text","text":""}}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_text.delta","sequence_number":3,"output_index":0,"item_id":"rs_upstream","summary_index":0,"delta":"checked"}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_text.done","sequence_number":4,"output_index":0,"item_id":"rs_upstream","summary_index":0,"text":"checked"}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_part.done","sequence_number":5,"output_index":0,"item_id":"rs_upstream","summary_index":0,"part":{"type":"summary_text","text":"checked"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.done","sequence_number":6,"output_index":0,"item":{"id":"rs_upstream","type":"reasoning","summary":[{"type":"summary_text","text":"checked"}],"encrypted_content":"`+encrypted+`","status":"completed"}}`),
		sdkStreamEvent(t, `{"type":"response.completed","sequence_number":7,"response":{"id":"r","model":"gpt","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":3}}}`),
	}
}

func recvAllSourceEvents(t *testing.T, source *eventSource) []canonical.Event {
	t.Helper()
	var events []canonical.Event
	for {
		event, err := source.Recv(context.Background())
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		events = append(events, event)
	}
}

func TestEventSourceRejectsMissingOrOversizedEncryptedReasoningBeforeVisibleOutput(t *testing.T) {
	for name, encrypted := range map[string]string{
		"missing":   "",
		"oversized": strings.Repeat("private-cipher-", canonical.MaxContinuationBytes/8),
	} {
		t.Run(name, func(t *testing.T) {
			source, err := newEventSourceWithAffinity(&fakeSDKStream{events: reasoningContinuationEvents(t, encrypted)}, "openai/public", "openai-primary", "gpt-upstream", canonical.OutputFormat("text"), &sourceIDs{})
			if err != nil {
				t.Fatal(err)
			}
			first, err := source.Recv(context.Background())
			if err != nil || first.Type() != canonical.EventType("response_started") {
				t.Fatalf("first event = %q, %v", first.Type(), err)
			}
			if _, err = source.Recv(context.Background()); err == nil {
				t.Fatal("invalid encrypted reasoning was accepted")
			}
			if strings.Contains(err.Error(), "private-cipher") {
				t.Fatal("error leaked encrypted content")
			}
		})
	}
}

func TestEventSourceRejectsCompletedTerminalBeforeReasoningDone(t *testing.T) {
	events := []sdkresponses.ResponseStreamEventUnion{
		sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"r","model":"gpt","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"rs_unfinished","type":"reasoning","summary":[],"status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.completed","sequence_number":2,"response":{"id":"r","model":"gpt","status":"completed","usage":{"input_tokens":1,"output_tokens":0,"total_tokens":1}}}`),
	}
	source, err := newEventSourceWithAffinity(&fakeSDKStream{events: events}, "openai/public", "openai-primary", "gpt-upstream", canonical.OutputFormat("text"), &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	if event, recvErr := source.Recv(context.Background()); recvErr != nil || event.Type() != canonical.EventType("response_started") {
		t.Fatalf("first event = %q, err=%v", event.Type(), recvErr)
	}
	if event, recvErr := source.Recv(context.Background()); execution.CodeOf(recvErr) != execution.CodeInternal || event.Valid() {
		t.Fatalf("unfinished reasoning terminal event = %#v, err=%v", event, recvErr)
	}
}

func TestEventSourceEmitsCumulativeContinuationUpdates(t *testing.T) {
	events := []sdkresponses.ResponseStreamEventUnion{
		sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"r","model":"gpt","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"rs_first","type":"reasoning","summary":[],"status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.done","sequence_number":2,"output_index":0,"item":{"id":"rs_first","type":"reasoning","summary":[],"encrypted_content":"cipher-first","status":"completed"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.added","sequence_number":3,"output_index":1,"item":{"id":"rs_second","type":"reasoning","summary":[],"status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.done","sequence_number":4,"output_index":1,"item":{"id":"rs_second","type":"reasoning","summary":[],"encrypted_content":"cipher-second","status":"completed"}}`),
		sdkStreamEvent(t, `{"type":"response.completed","sequence_number":5,"response":{"id":"r","model":"gpt","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":2}}}`),
	}
	source, err := newEventSourceWithAffinity(&fakeSDKStream{events: events}, "openai/public", "openai-primary", "gpt-upstream", canonical.OutputFormat("text"), &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	mapped := recvAllSourceEvents(t, source)
	var updates []canonical.Continuation
	var anchors []canonical.ItemID
	for _, event := range mapped {
		if payload, ok := event.ContinuationUpdated(); ok {
			updates = append(updates, payload.Continuation())
		}
		if payload, ok := event.ItemStarted(); ok {
			anchors = append(anchors, payload.ID())
		}
	}
	if len(updates) != 2 || len(anchors) != 2 {
		t.Fatalf("updates/anchors = %d/%d", len(updates), len(anchors))
	}
	entries, err := openaicontinuation.DecodeContinuation(updates[1], "openai-primary", "gpt-upstream", anchors)
	if err != nil || len(entries) != 2 || entries[0].UpstreamItemID() != "rs_first" || entries[1].UpstreamItemID() != "rs_second" {
		t.Fatalf("cumulative entries = %#v, %v", entries, err)
	}
}

func TestEventSourceBuffersReasoningUntilAuthoritativeEncryptedDone(t *testing.T) {
	source, err := newEventSourceWithAffinity(&fakeSDKStream{events: reasoningContinuationEvents(t, "cipher-marker")}, "openai/public", "openai-primary", "gpt-upstream", canonical.OutputFormat("text"), &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	events := recvAllSourceEvents(t, source)
	want := []canonical.EventType{"response_started", "continuation_updated", "item_started", "content_started", "text_delta", "content_completed", "item_completed", "response_completed"}
	if len(events) != len(want) {
		t.Fatalf("event count = %d, want %d", len(events), len(want))
	}
	for index, kind := range want {
		if events[index].Type() != kind {
			t.Fatalf("event[%d] = %q, want %q", index, events[index].Type(), kind)
		}
	}
	payload, _ := events[1].ContinuationUpdated()
	continuation := payload.Continuation()
	itemStart, _ := events[2].ItemStarted()
	entries, err := openaicontinuation.DecodeContinuation(continuation, "openai-primary", "gpt-upstream", []canonical.ItemID{itemStart.ID()})
	if err != nil || len(entries) != 1 {
		t.Fatalf("continuation entries = %#v, %v", entries, err)
	}
	if entries[0].CanonicalItemID() != itemStart.ID() || entries[0].UpstreamItemID() != "rs_upstream" || entries[0].EncryptedContent() != "cipher-marker" {
		t.Fatal("continuation entry did not preserve reasoning identity")
	}
	terminal, _ := events[len(events)-1].ResponseCompleted()
	_ = terminal
}
