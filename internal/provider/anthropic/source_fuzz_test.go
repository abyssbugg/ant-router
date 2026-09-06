package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

func FuzzAnthropicEventSourceRawEvent(f *testing.F) {
	f.Add(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	f.Add(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"private-fuzz-canary"}}`)
	f.Add(`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1}}`)
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 64<<10 {
			return
		}
		var candidate sdk.MessageStreamEventUnion
		if err := json.Unmarshal([]byte(raw), &candidate); err != nil {
			return
		}
		started := sdkStreamEvent(t, `{"type":"message_start","message":{"id":"msg_fuzz","type":"message","role":"assistant","content":[],"model":"claude-upstream","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0}}}`)
		source, err := newEventSource(&fakeSDKStream{events: []sdk.MessageStreamEventUnion{started, candidate}}, "anthropic/fuzz", canonical.OutputFormat("text"), &sourceIDs{})
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		for index := 0; index < 4; index++ {
			event, receiveErr := source.Recv(context.Background())
			if receiveErr != nil {
				if !errors.Is(receiveErr, io.EOF) && execution.CodeOf(receiveErr) == "" {
					t.Fatalf("unclassified stream error: %T", receiveErr)
				}
				if strings.Contains(receiveErr.Error(), "private-fuzz-canary") {
					t.Fatal("provider event value leaked")
				}
				return
			}
			if !event.Valid() {
				t.Fatal("invalid canonical event emitted")
			}
		}
	})
}
