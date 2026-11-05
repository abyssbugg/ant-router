package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

func (c *countingIDs) nextEvent(t *testing.T) canonical.EventID {
	t.Helper()
	id, err := c.EventID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func canonicalResponseWithToolCall(t *testing.T) canonical.Response {
	t.Helper()
	ids := &countingIDs{}
	text, err := canonical.NewTextContent("looking it up")
	if err != nil {
		t.Fatal(err)
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		t.Fatal(err)
	}
	itemID, err := ids.ItemID()
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
	if err != nil {
		t.Fatal(err)
	}
	messageItem, err := canonical.NewMessageItem(message)
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := canonicaljson.Parse([]byte(`{"q":"x"}`), canonicaljson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	callItemID, err := ids.ItemID()
	if err != nil {
		t.Fatal(err)
	}
	callID, err := ids.ToolCallID()
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCall(callItemID, callID, "lookup", arguments)
	if err != nil {
		t.Fatal(err)
	}
	callItem, err := canonical.NewToolCallItem(call)
	if err != nil {
		t.Fatal(err)
	}
	response, err := canonical.NewResponse(canonical.ResponseParams{
		ID: mustResponseID(t, ids), Model: "deepseek-chat",
		Items:        []canonical.Item{messageItem, callItem},
		FinishReason: canonical.FinishReason("tool_call"),
		Usage:        mustUsage(t, 10, 5, 15),
	})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func mustResponseID(t *testing.T, ids *countingIDs) canonical.ResponseID {
	t.Helper()
	id, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustUsage(t *testing.T, in, out, total int64) canonical.Usage {
	t.Helper()
	usage, err := canonical.NewUsage(canonical.UsageParams{InputTokens: in, OutputTokens: out, TotalTokens: total})
	if err != nil {
		t.Fatal(err)
	}
	return usage
}

func TestEncodeCompletionShape(t *testing.T) {
	response := canonicalResponseWithToolCall(t)
	encoded, err := EncodeCompletion(response, 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, want := range []string{
		`"object":"chat.completion"`,
		`"finish_reason":"tool_calls"`,
		`"name":"lookup"`,
		`"content":"looking it up"`,
		`"total_tokens":15`,
		`"model":"deepseek-chat"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("completion missing %s: %s", want, body)
		}
	}
}

type recordingChunkWriter struct {
	frames []string
}

func (w *recordingChunkWriter) WriteChunk(data string) error {
	w.frames = append(w.frames, data)
	return nil
}

func TestSSEStreamSinkMapsEventStreamToChatChunks(t *testing.T) {
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	writer := &recordingChunkWriter{}
	sink, err := NewSSEStreamSink(writer, "deepseek-chat", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	itemID, err := ids.ItemID()
	if err != nil {
		t.Fatal(err)
	}
	events := []struct {
		build func(sequence uint64) canonical.Event
	}{
		{func(seq uint64) canonical.Event {
			e, _ := canonical.NewResponseStartedEvent(ids.nextEvent(t), responseID, seq, "deepseek-chat")
			return e
		}},
		{func(seq uint64) canonical.Event {
			e, _ := canonical.NewItemStartedEvent(ids.nextEvent(t), responseID, seq, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
			return e
		}},
		{func(seq uint64) canonical.Event {
			e, _ := canonical.NewContentStartedEvent(ids.nextEvent(t), responseID, seq, itemID, 0, canonical.ContentType("text"))
			return e
		}},
		{func(seq uint64) canonical.Event {
			e, _ := canonical.NewTextDeltaEvent(ids.nextEvent(t), responseID, seq, itemID, 0, "Hel")
			return e
		}},
		{func(seq uint64) canonical.Event {
			e, _ := canonical.NewTextDeltaEvent(ids.nextEvent(t), responseID, seq, itemID, 0, "lo")
			return e
		}},
	}
	sequence := uint64(0)
	for _, step := range events {
		event := step.build(sequence)
		if _, err := machine.Accept(event); err != nil {
			t.Fatal(err)
		}
		if err := sink.Send(context.Background(), event); err != nil {
			t.Fatal(err)
		}
		sequence++
	}
	content, err := canonical.NewTextContent("Hello")
	if err != nil {
		t.Fatal(err)
	}
	block, err := canonical.NewTextBlock(content)
	if err != nil {
		t.Fatal(err)
	}
	contentDone, err := canonical.NewContentCompletedEvent(ids.nextEvent(t), responseID, sequence, itemID, 0, block)
	if err != nil {
		t.Fatal(err)
	}
	sequence++
	if _, err := machine.Accept(contentDone); err != nil {
		t.Fatal(err)
	}
	if err := sink.Send(context.Background(), contentDone); err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		t.Fatal(err)
	}
	itemDone, err := canonical.NewItemCompletedEvent(ids.nextEvent(t), responseID, sequence, 0, item)
	if err != nil {
		t.Fatal(err)
	}
	sequence++
	if _, err := machine.Accept(itemDone); err != nil {
		t.Fatal(err)
	}
	if err := sink.Send(context.Background(), itemDone); err != nil {
		t.Fatal(err)
	}
	completed, err := canonical.NewResponseCompletedEvent(ids.nextEvent(t), responseID, sequence, canonical.FinishReason("stop"), mustUsage(t, 3, 2, 5), []canonical.ItemID{itemID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.Accept(completed); err != nil {
		t.Fatal(err)
	}
	if err := sink.Send(context.Background(), completed); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(writer.frames, "")
	for _, want := range []string{
		`"role":"assistant"`,
		`"content":"Hel"`,
		`"content":"lo"`,
		`"finish_reason":"stop"`,
		`"total_tokens":5`,
		"data: [DONE]",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("sse missing %s: %s", want, joined)
		}
	}
	if strings.Count(joined, "data: [DONE]") != 1 {
		t.Fatal("[DONE] emitted more than once")
	}
}

func TestSSEStreamSinkFailsOnTerminalFailure(t *testing.T) {
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	writer := &recordingChunkWriter{}
	sink, err := NewSSEStreamSink(writer, "m", 1)
	if err != nil {
		t.Fatal(err)
	}
	start, err := canonical.NewResponseStartedEvent(ids.nextEvent(t), responseID, 0, "m")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.Accept(start); err != nil {
		t.Fatal(err)
	}
	failure, err := canonical.NewFailure(canonical.FailureParams{Code: canonical.FailureCode("upstream_protocol_error"), Message: "upstream refused"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := canonical.NewResponseFailedEvent(ids.nextEvent(t), responseID, 1, failure, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.Accept(failed); err != nil {
		t.Fatal(err)
	}
	if err := sink.Send(context.Background(), failed); err == nil {
		t.Fatal("sink accepted a failed terminal without an error")
	}
}
