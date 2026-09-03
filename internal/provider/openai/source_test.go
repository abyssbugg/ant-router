package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
	sdkresponses "github.com/openai/openai-go/v3/responses"
)

type fakeSDKStream struct {
	events    []sdkresponses.ResponseStreamEventUnion
	index     int
	err       error
	closeErr  error
	nextCalls int
	closes    int
}

func (s *fakeSDKStream) Next() bool {
	s.nextCalls++
	if s.index >= len(s.events) {
		return false
	}
	s.index++
	return s.index <= len(s.events)
}
func (s *fakeSDKStream) Current() sdkresponses.ResponseStreamEventUnion { return s.events[s.index-1] }
func (s *fakeSDKStream) Err() error                                     { return s.err }
func (s *fakeSDKStream) Close() error                                   { s.closes++; return s.closeErr }

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
func (g *sourceIDs) ItemID() (canonical.ItemID, error) { return canonical.GenerateItemID(g.reader()) }
func (g *sourceIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(g.reader())
}

func sdkStreamEvent(t *testing.T, document string) sdkresponses.ResponseStreamEventUnion {
	t.Helper()
	var event sdkresponses.ResponseStreamEventUnion
	if err := json.Unmarshal([]byte(document), &event); err != nil {
		t.Fatalf("decode SDK event: %v", err)
	}
	return event
}

func textSDKEvents(t *testing.T) []sdkresponses.ResponseStreamEventUnion {
	t.Helper()
	return []sdkresponses.ResponseStreamEventUnion{
		sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"resp_upstream","model":"gpt-upstream","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.in_progress","sequence_number":1,"response":{"id":"resp_upstream","model":"gpt-upstream","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.added","sequence_number":2,"output_index":0,"item":{"id":"msg_upstream","type":"message","role":"assistant","status":"in_progress","content":[]}}`),
		sdkStreamEvent(t, `{"type":"response.content_part.added","sequence_number":3,"output_index":0,"content_index":0,"item_id":"msg_upstream","part":{"type":"output_text","text":"","annotations":[]}}`),
		sdkStreamEvent(t, `{"type":"response.output_text.delta","sequence_number":4,"output_index":0,"content_index":0,"item_id":"msg_upstream","delta":"hello"}`),
		sdkStreamEvent(t, `{"type":"response.output_text.done","sequence_number":5,"output_index":0,"content_index":0,"item_id":"msg_upstream","text":"hello"}`),
		sdkStreamEvent(t, `{"type":"response.content_part.done","sequence_number":6,"output_index":0,"content_index":0,"item_id":"msg_upstream","part":{"type":"output_text","text":"hello","annotations":[]}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.done","sequence_number":7,"output_index":0,"item":{"id":"msg_upstream","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}}`),
		sdkStreamEvent(t, `{"type":"response.completed","sequence_number":8,"response":{"id":"resp_upstream","model":"gpt-upstream","status":"completed","usage":{"input_tokens":2,"input_tokens_details":{"cached_tokens":1,"cache_write_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":3}}}`),
	}
}

func TestEventSourceMapsCompleteTextStream(t *testing.T) {
	iterator := &fakeSDKStream{events: textSDKEvents(t)}
	source, err := newEventSource(iterator, "openai/public", canonical.OutputFormat("text"), &sourceIDs{})
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
	wantTypes := []canonical.EventType{"response_started", "item_started", "content_started", "text_delta", "content_completed", "item_completed", "response_completed"}
	if len(eventTypes) != len(wantTypes) {
		t.Fatalf("event types = %v", eventTypes)
	}
	for index := range wantTypes {
		if eventTypes[index] != wantTypes[index] {
			t.Fatalf("event[%d] = %q, want %q", index, eventTypes[index], wantTypes[index])
		}
	}
	response, ok := final.Response()
	if !ok || response.Model() != "openai/public" || response.Usage().InputTokens() != 2 || response.Usage().CacheReadTokens() != 1 || response.Usage().OutputTokens() != 1 {
		t.Fatalf("response = %#v, ok=%v", response, ok)
	}
	message, _ := response.Items()[0].Message()
	text, _ := message.Content()[0].Text()
	if text.Text() != "hello" {
		t.Fatalf("text = %q", text.Text())
	}
}

func TestEventSourceRejectsProviderSequenceOverflow(t *testing.T) {
	source := &eventSource{providerSequence: math.MaxInt64, hasProviderEvent: true}
	if err := source.acceptProviderSequence(math.MinInt64); execution.CodeOf(err) != execution.CodeInternal {
		t.Fatalf("overflow error = %v", err)
	}
	if source.providerSequence != math.MaxInt64 {
		t.Fatalf("sequence mutated to %d", source.providerSequence)
	}
}

func TestEventSourceRejectsSequenceGapsAndUnsupportedEvents(t *testing.T) {
	for _, test := range []struct {
		name   string
		events []sdkresponses.ResponseStreamEventUnion
	}{
		{"gap", []sdkresponses.ResponseStreamEventUnion{
			sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"r","model":"m","status":"in_progress"}}`),
			sdkStreamEvent(t, `{"type":"response.in_progress","sequence_number":2,"response":{"id":"r","model":"m","status":"in_progress"}}`),
		}},
		{"unsupported", []sdkresponses.ResponseStreamEventUnion{sdkStreamEvent(t, `{"type":"response.web_search_call.in_progress","sequence_number":0}`)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, err := newEventSource(&fakeSDKStream{events: test.events}, "openai/public", canonical.OutputFormat("text"), &sourceIDs{})
			if err != nil {
				t.Fatal(err)
			}
			for {
				_, err = source.Recv(context.Background())
				if err != nil {
					break
				}
			}
			if execution.CodeOf(err) != execution.CodeInternal {
				t.Fatalf("error = %v, code=%q", err, execution.CodeOf(err))
			}
		})
	}
}

type failingEventIDs struct {
	sourceIDs
	failAt int
	calls  int
}

func (g *failingEventIDs) EventID() (canonical.EventID, error) {
	g.calls++
	if g.calls == g.failAt {
		return canonical.EventID(""), errors.New("private ID failure canary")
	}
	return g.sourceIDs.EventID()
}

func TestEventSourceIDFailureLeavesStateTransactional(t *testing.T) {
	t.Run("response start", func(t *testing.T) {
		source, err := newEventSource(&fakeSDKStream{events: textSDKEvents(t)}, "openai/public", canonical.OutputFormat("text"), &failingEventIDs{failAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = source.Recv(context.Background()); execution.CodeOf(err) != execution.CodeInternal {
			t.Fatalf("error = %v", err)
		}
		if source.started || source.canonicalSequence != 0 || len(source.items) != 0 {
			t.Fatalf("state mutated: started=%v sequence=%d items=%d", source.started, source.canonicalSequence, len(source.items))
		}
	})

	t.Run("content completion", func(t *testing.T) {
		source, err := newEventSource(&fakeSDKStream{events: textSDKEvents(t)}, "openai/public", canonical.OutputFormat("text"), &failingEventIDs{failAt: 5})
		if err != nil {
			t.Fatal(err)
		}
		for index := 0; index < 4; index++ {
			if _, err = source.Recv(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = source.Recv(context.Background()); execution.CodeOf(err) != execution.CodeInternal {
			t.Fatalf("error = %v", err)
		}
		state := source.items["msg_upstream"]
		if state == nil || len(state.contents) != 0 || source.canonicalSequence != 4 {
			t.Fatalf("state mutated: state=%#v sequence=%d", state, source.canonicalSequence)
		}
	})

	t.Run("tool delta", func(t *testing.T) {
		source, err := newEventSource(&fakeSDKStream{events: toolSDKEvents(t)}, "openai/public", canonical.OutputFormat("text"), &failingEventIDs{failAt: 4})
		if err != nil {
			t.Fatal(err)
		}
		for index := 0; index < 3; index++ {
			if _, err = source.Recv(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = source.Recv(context.Background()); execution.CodeOf(err) != execution.CodeInternal {
			t.Fatalf("error = %v", err)
		}
		state := source.items["fc_upstream"]
		if state == nil || state.arguments != "" || source.canonicalSequence != 3 {
			t.Fatalf("state mutated: state=%#v sequence=%d", state, source.canonicalSequence)
		}
	})
}

func TestEventSourceProtocolFailurePoisonsStream(t *testing.T) {
	iterator := &fakeSDKStream{events: []sdkresponses.ResponseStreamEventUnion{
		sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"r","model":"m","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.in_progress","sequence_number":2,"response":{"id":"r","model":"m","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.in_progress","sequence_number":3,"response":{"id":"r","model":"m","status":"in_progress"}}`),
	}}
	source, err := newEventSource(iterator, "openai/public", canonical.OutputFormat("text"), &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.Recv(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, first := source.Recv(context.Background())
	calls := iterator.nextCalls
	_, second := source.Recv(context.Background())
	if execution.CodeOf(first) != execution.CodeInternal || execution.CodeOf(second) != execution.CodeInternal || iterator.nextCalls != calls {
		t.Fatalf("fatal errors/calls = %v/%v/%d->%d", first, second, calls, iterator.nextCalls)
	}
}

func TestEventSourceCancellationIteratorErrorAndClose(t *testing.T) {
	iterator := &fakeSDKStream{err: errors.New("private stream canary")}
	source, err := newEventSource(iterator, "openai/public", canonical.OutputFormat("text"), &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Recv(ctx); !errors.Is(err, context.Canceled) || iterator.nextCalls != 0 {
		t.Fatalf("cancelled Recv = %v, calls=%d", err, iterator.nextCalls)
	}
	_, err = source.Recv(context.Background())
	if execution.CodeOf(err) != execution.CodeInternal || bytes.Contains([]byte(err.Error()), []byte("private stream canary")) {
		t.Fatalf("iterator error = %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if iterator.closes != 1 {
		t.Fatalf("close calls = %d", iterator.closes)
	}
}

var _ streaming.EventSource = (*eventSource)(nil)
