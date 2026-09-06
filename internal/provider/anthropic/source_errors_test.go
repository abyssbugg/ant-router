package anthropic

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

func TestEventSourceRejectsMalformedAndUnsupportedEvents(t *testing.T) {
	tests := map[string][]sdk.MessageStreamEventUnion{
		"unknown event":       {sdkStreamEvent(t, `{"type":"mystery"}`)},
		"delta before start":  {messageStart(t), sdkStreamEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`)},
		"noncontiguous index": {messageStart(t), sdkStreamEvent(t, `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)},
		"thinking":            {messageStart(t), sdkStreamEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`)},
		"duplicate start":     {messageStart(t), sdkStreamEvent(t, `{"type":"message_start","message":{"id":"again","type":"message","role":"assistant","content":[],"model":"claude-test","usage":{"input_tokens":1,"output_tokens":0}}}`)},
		"stop before delta":   {messageStart(t), sdkStreamEvent(t, `{"type":"message_stop"}`)},
	}
	for name, events := range tests {
		t.Run(name, func(t *testing.T) {
			source, err := newEventSource(&fakeSDKStream{events: events}, "anthropic/public", canonical.OutputFormat("text"), &sourceIDs{})
			if err != nil {
				t.Fatalf("newEventSource: %v", err)
			}
			var got error
			for got == nil {
				_, got = source.Recv(context.Background())
			}
			if errors.Is(got, io.EOF) {
				t.Fatalf("got EOF, want validation error")
			}
		})
	}
}

func TestEventSourcePreservesCancellationBeforeRead(t *testing.T) {
	stream := &fakeSDKStream{events: textSDKEvents(t)}
	source, err := newEventSource(stream, "anthropic/public", canonical.OutputFormat("text"), &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Recv(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if stream.index != 0 {
		t.Fatalf("stream advanced to %d", stream.index)
	}
}

func TestEventSourceRedactsUpstreamError(t *testing.T) {
	const marker = "provider-secret-marker"
	source, err := newEventSource(&fakeSDKStream{err: errors.New(marker)}, "anthropic/public", canonical.OutputFormat("text"), &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	_, got := source.Recv(context.Background())
	if got == nil || strings.Contains(got.Error(), marker) {
		t.Fatalf("error = %v", got)
	}
}

func TestEventSourceRejectsInvalidMessageStartState(t *testing.T) {
	event := sdkStreamEvent(t, `{"type":"message_start","message":{"id":"msg","type":"message","role":"assistant","content":[{"type":"text","text":"already"}],"model":"claude-test","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}}`)
	source, err := newEventSource(&fakeSDKStream{events: []sdk.MessageStreamEventUnion{event}}, "anthropic/public", canonical.OutputFormat("text"), &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Recv(context.Background()); err == nil {
		t.Fatal("invalid message_start accepted")
	}
}

func TestEventSourceRejectsNegativeDeltaAndStopIndices(t *testing.T) {
	for name, event := range map[string]sdk.MessageStreamEventUnion{
		"delta": sdkStreamEvent(t, `{"type":"content_block_delta","index":-1,"delta":{"type":"text_delta","text":"x"}}`),
		"stop":  sdkStreamEvent(t, `{"type":"content_block_stop","index":-1}`),
	} {
		t.Run(name, func(t *testing.T) {
			source, err := newEventSource(&fakeSDKStream{events: append([]sdk.MessageStreamEventUnion{messageStart(t)}, event)}, "anthropic/public", canonical.OutputFormat("text"), &sourceIDs{})
			if err != nil {
				t.Fatal(err)
			}
			_, _ = source.Recv(context.Background())
			_, _ = source.Recv(context.Background())
			if _, err := source.Recv(context.Background()); err == nil {
				t.Fatal("invalid index accepted")
			}
		})
	}
}

func TestEventSourceRejectsDecreasingUsage(t *testing.T) {
	start := sdkStreamEvent(t, `{"type":"message_start","message":{"id":"msg","type":"message","role":"assistant","content":[],"model":"claude-test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":2}}}`)
	delta := sdkStreamEvent(t, `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`)
	source, err := newEventSource(&fakeSDKStream{events: []sdk.MessageStreamEventUnion{start, delta}}, "anthropic/public", canonical.OutputFormat("text"), &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = source.Recv(context.Background())
	_, _ = source.Recv(context.Background())
	if _, err := source.Recv(context.Background()); err == nil {
		t.Fatal("decreasing usage accepted")
	}
	if source.hasFinish {
		t.Fatal("decreasing usage mutated finish state")
	}
}

type failingSourceIDs struct{ sourceIDs }

func (*failingSourceIDs) ResponseID() (canonical.ResponseID, error) {
	return canonical.ResponseID(""), errors.New("id failure")
}

func TestEventSourceRejectsIDFailureWithoutAdvancing(t *testing.T) {
	source, err := newEventSource(&fakeSDKStream{events: []sdk.MessageStreamEventUnion{messageStart(t)}}, "anthropic/public", canonical.OutputFormat("text"), &failingSourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Recv(context.Background()); err == nil {
		t.Fatal("expected ID error")
	}
	if source.started || source.sequence != 0 {
		t.Fatalf("source mutated: %#v", source)
	}
}
