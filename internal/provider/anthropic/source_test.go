package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

type fakeSDKStream struct {
	events   []sdk.MessageStreamEventUnion
	index    int
	err      error
	closeErr error
	closes   int
}

func (s *fakeSDKStream) Next() bool {
	if s.index >= len(s.events) {
		return false
	}
	s.index++
	return s.index <= len(s.events)
}

func (s *fakeSDKStream) Current() sdk.MessageStreamEventUnion {
	if s.index > 0 && s.index <= len(s.events) {
		return s.events[s.index-1]
	}
	return sdk.MessageStreamEventUnion{}
}

func (s *fakeSDKStream) Err() error   { return s.err }
func (s *fakeSDKStream) Close() error { s.closes++; return s.closeErr }

type sourceIDs struct{ next byte }

func (g *sourceIDs) reader() *bytes.Reader {
	g.next++
	return bytes.NewReader(bytes.Repeat([]byte{g.next}, canonical.IDRandomBytes))
}

func (g *sourceIDs) ResponseID() (canonical.ResponseID, error) {
	return canonical.GenerateResponseID(g.reader())
}

func (g *sourceIDs) EventID() (canonical.EventID, error) {
	return canonical.GenerateEventID(g.reader())
}

func (g *sourceIDs) ItemID() (canonical.ItemID, error) {
	return canonical.GenerateItemID(g.reader())
}

func (g *sourceIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(g.reader())
}

func sdkStreamEvent(t *testing.T, document string) sdk.MessageStreamEventUnion {
	t.Helper()
	var event sdk.MessageStreamEventUnion
	if err := json.Unmarshal([]byte(document), &event); err != nil {
		t.Fatalf("decode SDK event: %v", err)
	}
	return event
}

func textSDKEvents(t *testing.T) []sdk.MessageStreamEventUnion {
	t.Helper()
	return []sdk.MessageStreamEventUnion{
		sdkStreamEvent(t, `{"type":"message_start","message":{"id":"msg_upstream","type":"message","role":"assistant","content":[],"model":"claude-3-5-sonnet-20241022","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`),
		sdkStreamEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sdkStreamEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`),
		sdkStreamEvent(t, `{"type":"content_block_stop","index":0}`),
		sdkStreamEvent(t, `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`),
		sdkStreamEvent(t, `{"type":"message_stop"}`),
	}
}

func TestEventSourceMapsCompleteTextStream(t *testing.T) {
	iterator := &fakeSDKStream{events: textSDKEvents(t)}
	source, err := newEventSource(iterator, "claude/public", canonical.OutputFormat("text"), &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	machine, _ := streaming.NewMachine(streaming.DefaultMachineOptions())
	var eventTypes []canonical.EventType
	var final streaming.Transition
	for {
		event, err := source.Recv(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		eventTypes = append(eventTypes, event.Type())
		final, err = machine.Accept(event)
		if err != nil {
			t.Fatalf("machine Accept(%s): %v", event.Type(), err)
		}
	}

	expected := []canonical.EventType{
		canonical.EventType("response_started"),
		canonical.EventType("usage_updated"),
		canonical.EventType("item_started"),
		canonical.EventType("content_started"),
		canonical.EventType("text_delta"),
		canonical.EventType("content_completed"),
		canonical.EventType("usage_updated"),
		canonical.EventType("item_completed"),
		canonical.EventType("response_completed"),
	}
	if len(eventTypes) != len(expected) {
		t.Errorf("got %d events, want %d: %v", len(eventTypes), len(expected), eventTypes)
	}
	for i, et := range eventTypes {
		if i < len(expected) && et != expected[i] {
			t.Errorf("event %d: got %s, want %s", i, et, expected[i])
		}
	}

	if final.TerminalKind() == streaming.TerminalNone {
		t.Errorf("final transition not terminal: %v", final)
	}
}

func TestEventSourceCloseExactlyOnce(t *testing.T) {
	iterator := &fakeSDKStream{events: textSDKEvents(t)}
	source, _ := newEventSource(iterator, "claude/public", canonical.OutputFormat("text"), &sourceIDs{})

	// Drain events
	for {
		_, err := source.Recv(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	// Close multiple times
	source.Close()
	source.Close()
	source.Close()

	if iterator.closes != 1 {
		t.Errorf("Close called %d times, want 1", iterator.closes)
	}
}

func TestEventSourceCancelledContextBeforeRead(t *testing.T) {
	iterator := &fakeSDKStream{events: textSDKEvents(t)}
	source, _ := newEventSource(iterator, "claude/public", canonical.OutputFormat("text"), &sourceIDs{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := source.Recv(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestEventSourceMessageStartExactlyOnce(t *testing.T) {
	iterator := &fakeSDKStream{events: textSDKEvents(t)}
	source, _ := newEventSource(iterator, "claude/public", canonical.OutputFormat("text"), &sourceIDs{})

	var responseStartedCount int
	for {
		event, err := source.Recv(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if event.Type() == canonical.EventType("response_started") {
			responseStartedCount++
		}
	}

	if responseStartedCount != 1 {
		t.Errorf("response_started events: %d, want 1", responseStartedCount)
	}
}

func TestEventSourceNilStream(t *testing.T) {
	_, err := newEventSource(nil, "claude/public", canonical.OutputFormat("text"), &sourceIDs{})
	if err == nil {
		t.Error("expected error for nil stream")
	}
}

func TestEventSourceInvalidModel(t *testing.T) {
	iterator := &fakeSDKStream{}
	_, err := newEventSource(iterator, "", canonical.OutputFormat("text"), &sourceIDs{})
	if err == nil {
		t.Error("expected error for empty model")
	}
}

func TestEventSourceInvalidFormat(t *testing.T) {
	iterator := &fakeSDKStream{}
	_, err := newEventSource(iterator, "claude/public", canonical.OutputFormat(""), &sourceIDs{})
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestEventSourceNilIDGenerator(t *testing.T) {
	iterator := &fakeSDKStream{}
	_, err := newEventSource(iterator, "claude/public", canonical.OutputFormat("text"), nil)
	if err == nil {
		t.Error("expected error for nil IDs")
	}
}
