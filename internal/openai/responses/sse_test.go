package responses

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/provider/mock"
)

func TestSSESinkMapsTextStreamAndFlushes(t *testing.T) {
	request := decodedRequest(t, "mock/text")
	ids := &outputIDs{}
	provider, err := mock.New(mock.Options{IDs: ids, SlowDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	source, err := provider.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	encoder, _ := NewEncoder(time.Unix(1_700_000_000, 0))
	writer := &flushBuffer{}
	sink, err := NewSSESink(writer, encoder)
	if err != nil {
		t.Fatal(err)
	}
	for {
		event, recvErr := source.Recv(context.Background())
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			t.Fatal(recvErr)
		}
		if err := sink.Send(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	output := writer.String()
	for _, eventType := range []string{
		"response.created", "response.output_item.added", "response.content_part.added",
		"response.output_text.delta", "response.output_text.done", "response.output_item.done",
		"response.in_progress", "response.completed",
	} {
		if !strings.Contains(output, "event: "+eventType+"\n") || !strings.Contains(output, `"type":"`+eventType+`"`) {
			t.Fatalf("missing %s in %s", eventType, output)
		}
	}
	if !strings.HasSuffix(output, "data: [DONE]\n\n") {
		t.Fatalf("missing terminal marker: %s", output)
	}
	if writer.flushes != 9 {
		t.Fatalf("flushes = %d, want 9", writer.flushes)
	}
}

func TestSSESinkMapsJSONToolAndReasoningStreams(t *testing.T) {
	cases := map[string][]string{
		"mock/json":      {"response.output_text.delta", "response.output_text.done"},
		"mock/tool":      {"response.function_call_arguments.delta", "response.output_item.done"},
		"mock/reasoning": {"response.reasoning_summary_part.added", "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done"},
	}
	for model, wantEvents := range cases {
		t.Run(model, func(t *testing.T) {
			provider, _ := mock.New(mock.Options{IDs: &outputIDs{}})
			source, err := provider.Execute(context.Background(), decodedRequest(t, model))
			if err != nil {
				t.Fatal(err)
			}
			encoder, _ := NewEncoder(time.Unix(1, 0))
			writer := &flushBuffer{}
			sink, _ := NewSSESink(writer, encoder)
			for {
				event, recvErr := source.Recv(context.Background())
				if errors.Is(recvErr, io.EOF) {
					break
				}
				if recvErr != nil {
					t.Fatal(recvErr)
				}
				if err := sink.Send(context.Background(), event); err != nil {
					t.Fatal(err)
				}
			}
			for _, eventType := range wantEvents {
				if !strings.Contains(writer.String(), "event: "+eventType+"\n") {
					t.Fatalf("missing %s in %s", eventType, writer.String())
				}
			}
			if model == "mock/reasoning" && !strings.Contains(writer.String(), `"summary_index":0`) {
				t.Fatalf("reasoning summary index missing: %s", writer.String())
			}
			if !strings.HasSuffix(writer.String(), "data: [DONE]\n\n") {
				t.Fatal("terminal marker missing")
			}
		})
	}
}

func TestSSESinkMapsFailedAndCancelledTerminals(t *testing.T) {
	for _, tc := range []struct {
		name, eventName string
		code            canonical.FailureCode
	}{
		{"failed", "response.failed", canonical.FailureCode("internal_error")},
		{"cancelled", "response.incomplete", canonical.FailureCode("cancelled")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := &outputIDs{}
			responseID, _ := ids.ResponseID()
			startedID, _ := ids.EventID()
			terminalID, _ := ids.EventID()
			started, _ := canonical.NewResponseStartedEvent(startedID, responseID, 0, "mock/text")
			failure, _ := canonical.NewFailure(canonical.FailureParams{Code: tc.code, Message: "safe failure"})
			var terminal canonical.Event
			if tc.code == canonical.FailureCode("cancelled") {
				terminal, _ = canonical.NewResponseCancelledEvent(terminalID, responseID, 1, failure, nil)
			} else {
				terminal, _ = canonical.NewResponseFailedEvent(terminalID, responseID, 1, failure, nil)
			}
			encoder, _ := NewEncoder(time.Unix(7, 0))
			writer := &flushBuffer{}
			sink, _ := NewSSESink(writer, encoder)
			if err := sink.Send(context.Background(), started); err != nil {
				t.Fatal(err)
			}
			if err := sink.Send(context.Background(), terminal); err != nil {
				t.Fatal(err)
			}
			output := writer.String()
			if !strings.Contains(output, "event: "+tc.eventName+"\n") || !strings.Contains(output, `"created_at":7`) || !strings.Contains(output, `"model":"mock/text"`) || !strings.HasSuffix(output, "data: [DONE]\n\n") {
				t.Fatalf("terminal output = %s", output)
			}
		})
	}
}

func TestSSESinkRejectsCancellationWriteFailureAndBadSequence(t *testing.T) {
	encoder, _ := NewEncoder(time.Unix(1, 0))
	writer := &flushBuffer{}
	sink, _ := NewSSESink(writer, encoder)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sink.Send(ctx, canonical.Event{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Send = %v", err)
	}

	request := decodedRequest(t, "mock/text")
	provider, _ := mock.New(mock.Options{IDs: &outputIDs{}})
	source, _ := provider.Execute(context.Background(), request)
	first, _ := source.Recv(context.Background())
	failing, _ := NewSSESink(&failWriter{}, encoder)
	if err := failing.Send(context.Background(), first); CodeOf(err) != CodeInternal || PathOf(err) != "stream.write" {
		t.Fatalf("write failure = %v", err)
	}
	short, _ := NewSSESink(&shortWriter{}, encoder)
	if err := short.Send(context.Background(), first); CodeOf(err) != CodeInternal || PathOf(err) != "stream.write" {
		t.Fatalf("short write = %v", err)
	}
	valid, _ := NewSSESink(&flushBuffer{}, encoder)
	if err := valid.Send(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := valid.Send(context.Background(), first); CodeOf(err) != CodeInternal || PathOf(err) != "stream.event" {
		t.Fatalf("bad sequence = %v", err)
	}
}

func TestNewSSESinkRejectsTypedNilWriter(t *testing.T) {
	encoder, _ := NewEncoder(time.Unix(1, 0))
	var writer *flushBuffer
	if _, err := NewSSESink(writer, encoder); CodeOf(err) != CodeInternal {
		t.Fatalf("typed-nil writer error = %v", err)
	}
}

func decodedRequest(t *testing.T, model string) canonical.Request {
	t.Helper()
	body := `{"model":"` + model + `","input":"hello"}`
	switch model {
	case "mock/tool":
		body = `{"model":"mock/tool","input":"hello","tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}]}`
	case "mock/json":
		body = `{"model":"mock/json","input":"hello","text":{"format":{"type":"json_object"}}}`
	case "mock/reasoning":
		body = `{"model":"mock/reasoning","input":"hello","reasoning":{"summary":"auto"}}`
	}
	parsed, err := Decode(strings.NewReader(body), &sequenceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Request
}

type flushBuffer struct {
	bytes.Buffer
	flushes int
}

func (w *flushBuffer) Flush() { w.flushes++ }

type failWriter struct{}

func (*failWriter) Write([]byte) (int, error) { return 0, errors.New("sensitive downstream failure") }
func (*failWriter) Flush()                    {}

type shortWriter struct{}

func (*shortWriter) Write([]byte) (int, error) { return 1, nil }
func (*shortWriter) Flush()                    {}
