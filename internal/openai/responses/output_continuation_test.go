package responses

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	openaicontinuation "github.com/abyssbugg/ant-router/internal/openai/continuation"
)

const (
	outputProviderID    = "openai-primary"
	outputUpstreamModel = "gpt-5.2"
	outputReasoningID   = "rs_upstream_original"
)

func encryptedReasoningResponse(t *testing.T, encrypted string) (canonical.Response, canonical.ItemID) {
	t.Helper()
	response := outputResponse(t, "reasoning")
	itemID := response.Items()[0].ID()
	entry, err := openaicontinuation.NewContinuationEntry(itemID, outputReasoningID, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	value, err := openaicontinuation.EncodeContinuation(outputProviderID, outputUpstreamModel, []openaicontinuation.ContinuationEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	response, err = response.WithContinuation(value)
	if err != nil {
		t.Fatal(err)
	}
	return response, itemID
}

func TestEncodeResponseEmitsUpstreamFunctionCallIdentity(t *testing.T) {
	response := outputResponse(t, "tool")
	item := response.Items()[0]
	call, _ := item.ToolCall()
	entry, err := openaicontinuation.NewFunctionCallEntry(item.ID(), call.CallID(), "fc_upstream", "call_upstream")
	if err != nil {
		t.Fatal(err)
	}
	continuation, err := openaicontinuation.EncodeContinuation("openai-primary", "gpt-upstream", []openaicontinuation.ContinuationEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	response, err = response.WithContinuation(continuation)
	if err != nil {
		t.Fatal(err)
	}
	encoder, _ := NewEncoder(time.Unix(1700000000, 0))
	encoded, err := encoder.EncodeResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	output := document["output"].([]any)[0].(map[string]any)
	if output["id"] != "fc_upstream" || output["call_id"] != "call_upstream" {
		t.Fatalf("function call identity = %#v", output)
	}
}

func TestEncodeResponseEmitsEncryptedReasoningWithUpstreamID(t *testing.T) {
	const encrypted = "encrypted-output-secret"
	response, itemID := encryptedReasoningResponse(t, encrypted)
	encoder, _ := NewEncoder(time.Unix(1_700_000_000, 0))
	encoded, err := encoder.EncodeResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	item := wire["output"].([]any)[0].(map[string]any)
	if item["id"] != outputReasoningID || item["encrypted_content"] != encrypted {
		t.Fatalf("reasoning output = %#v", item)
	}
	if strings.Contains(string(encoded), wireItemID("rs", itemID)) {
		t.Fatal("router-generated reasoning ID leaked into provider-affine output")
	}
}

func reasoningContinuationEvents(t *testing.T, encrypted string) ([]canonical.Event, canonical.ItemID) {
	t.Helper()
	response, itemID := encryptedReasoningResponse(t, encrypted)
	item := response.Items()[0]
	value, _ := response.Continuation()
	ids := &outputIDs{}
	newEventID := func() canonical.EventID {
		id, err := ids.EventID()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	started, _ := canonical.NewResponseStartedEvent(newEventID(), response.ID(), 0, response.Model())
	updated, _ := canonical.NewContinuationUpdatedEvent(newEventID(), response.ID(), 1, value)
	itemStarted, _ := canonical.NewItemStartedEvent(newEventID(), response.ID(), 2, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("reasoning_summary")})
	reasoning, _ := item.ReasoningSummary()
	block, _ := canonical.NewTextBlock(reasoning.Summaries()[0])
	contentStarted, _ := canonical.NewContentStartedEvent(newEventID(), response.ID(), 3, itemID, 0, canonical.ContentType("text"))
	contentCompleted, _ := canonical.NewContentCompletedEvent(newEventID(), response.ID(), 4, itemID, 0, block)
	itemCompleted, _ := canonical.NewItemCompletedEvent(newEventID(), response.ID(), 5, 0, item)
	completed, _ := canonical.NewResponseCompletedEvent(newEventID(), response.ID(), 6, response.FinishReason(), response.Usage(), []canonical.ItemID{itemID})
	return []canonical.Event{started, updated, itemStarted, contentStarted, contentCompleted, itemCompleted, completed}, itemID
}

func TestSSESinkConsumesContinuationSilentlyAndUsesUpstreamReasoningID(t *testing.T) {
	const encrypted = "encrypted-sse-secret"
	events, itemID := reasoningContinuationEvents(t, encrypted)
	encoder, _ := NewEncoder(time.Unix(7, 0))
	writer := &flushBuffer{}
	sink, _ := NewSSESink(writer, encoder)
	for _, event := range events {
		if !event.Valid() {
			t.Fatalf("constructed %s event is invalid", event.Type())
		}
		if err := sink.Send(context.Background(), event); err != nil {
			t.Fatalf("%s: %v", event.Type(), err)
		}
	}
	output := writer.String()
	if strings.Contains(output, "continuation_updated") {
		t.Fatal("internal continuation event leaked to SSE")
	}
	if strings.Contains(output, wireItemID("rs", itemID)) {
		t.Fatal("router-generated reasoning ID leaked to SSE")
	}
	if strings.Count(output, `"id":"`+outputReasoningID+`"`) < 3 || strings.Count(output, `"encrypted_content":"`+encrypted+`"`) < 3 {
		t.Fatalf("reasoning identity or ciphertext missing from SSE: %s", output)
	}
	if writer.flushes != 7 || !strings.HasSuffix(output, "data: [DONE]\n\n") {
		t.Fatalf("flushes=%d output=%s", writer.flushes, output)
	}
}

func TestSSESinkRejectsMalformedContinuationWithoutLeak(t *testing.T) {
	const marker = "private-malformed-cipher"
	responseID, _ := canonical.GenerateResponseID(strings.NewReader("0123456789abcdef"))
	ids := &outputIDs{}
	startedID, _ := ids.EventID()
	updatedID, _ := ids.EventID()
	started, _ := canonical.NewResponseStartedEvent(startedID, responseID, 0, "openai/model")
	value, _ := canonical.NewContinuation(outputProviderID, outputUpstreamModel, []byte(marker))
	updated, _ := canonical.NewContinuationUpdatedEvent(updatedID, responseID, 1, value)
	encoder, _ := NewEncoder(time.Unix(8, 0))
	writer := &flushBuffer{}
	sink, _ := NewSSESink(writer, encoder)
	if err := sink.Send(context.Background(), started); err != nil {
		t.Fatal(err)
	}
	err := sink.Send(context.Background(), updated)
	if CodeOf(err) != CodeInternal || strings.Contains(err.Error(), marker) || strings.Contains(writer.String(), marker) {
		t.Fatalf("malformed continuation error = %v", err)
	}
}

func TestEncodeResponseRejectsDanglingContinuationWithoutLeak(t *testing.T) {
	const encrypted = "dangling-private-cipher"
	response := outputResponse(t, "reasoning")
	otherID, _ := canonical.GenerateItemID(strings.NewReader("0123456789abcdef"))
	entry, _ := openaicontinuation.NewContinuationEntry(otherID, outputReasoningID, encrypted)
	value, _ := openaicontinuation.EncodeContinuation(outputProviderID, outputUpstreamModel, []openaicontinuation.ContinuationEntry{entry})
	response, _ = response.WithContinuation(value)
	encoder, _ := NewEncoder(time.Unix(1, 0))
	_, err := encoder.EncodeResponse(response)
	if CodeOf(err) != CodeInternal || strings.Contains(err.Error(), encrypted) {
		t.Fatalf("dangling continuation error = %v", err)
	}
}
