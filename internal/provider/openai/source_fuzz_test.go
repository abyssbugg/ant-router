package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	sdkresponses "github.com/openai/openai-go/v3/responses"
)

func FuzzOpenAIEventSourceRawEvent(f *testing.F) {
	f.Add(`{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"msg_fuzz","type":"message","role":"assistant","status":"in_progress","content":[]}}`)
	f.Add(`{"type":"response.error","sequence_number":1,"code":"server_error","message":"private-fuzz-canary"}`)
	f.Add(`{"type":"response.completed","sequence_number":1,"response":{"id":"resp_fuzz","model":"gpt-upstream","status":"completed","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 64<<10 {
			return
		}
		var candidate sdkresponses.ResponseStreamEventUnion
		if err := json.Unmarshal([]byte(raw), &candidate); err != nil {
			return
		}
		created := sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"resp_fuzz","model":"gpt-upstream","status":"in_progress"}}`)
		source, err := newEventSource(&fakeSDKStream{events: []sdkresponses.ResponseStreamEventUnion{created, candidate}}, "openai/fuzz", canonical.OutputFormat("text"), &sourceIDs{})
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		for index := 0; index < 3; index++ {
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
