package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func TestEncodeResponseMapsIncompleteFinishReason(t *testing.T) {
	encoder, err := NewEncoder(time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		finish canonical.FinishReason
		status string
		detail string
	}{
		{canonical.FinishReason("stop"), "completed", ""},
		{canonical.FinishReason("tool_call"), "completed", ""},
		{canonical.FinishReason("other"), "completed", ""},
		{canonical.FinishReason("length"), "incomplete", "max_output_tokens"},
		{canonical.FinishReason("content_filter"), "incomplete", "content_filter"},
	} {
		responseID, err := canonical.GenerateResponseID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
		if err != nil {
			t.Fatal(err)
		}
		items := []canonical.Item{}
		if tc.finish == canonical.FinishReason("tool_call") {
			itemID, err := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
			if err != nil {
				t.Fatal(err)
			}
			callID, err := canonical.GenerateToolCallID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
			if err != nil {
				t.Fatal(err)
			}
			arguments, err := canonicaljson.Parse([]byte(`{}`), canonicaljson.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			call, err := canonical.NewToolCall(itemID, callID, "tool", arguments)
			if err != nil {
				t.Fatal(err)
			}
			item, err := canonical.NewToolCallItem(call)
			if err != nil {
				t.Fatal(err)
			}
			items = append(items, item)
		}
		response, err := canonical.NewResponse(canonical.ResponseParams{ID: responseID, Model: "model", Items: items, FinishReason: tc.finish, Usage: canonical.Usage{}})
		if err != nil {
			t.Fatalf("finish %q: %v", tc.finish, err)
		}
		encoded, err := encoder.EncodeResponse(response)
		if err != nil {
			t.Fatalf("finish %q: %v", tc.finish, err)
		}
		var wire struct {
			Status            string `json:"status"`
			IncompleteDetails *struct {
				Reason string `json:"reason"`
			} `json:"incomplete_details"`
		}
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		if wire.Status != tc.status {
			t.Fatalf("finish %q status = %q, want %q", tc.finish, wire.Status, tc.status)
		}
		gotDetail := ""
		if wire.IncompleteDetails != nil {
			gotDetail = wire.IncompleteDetails.Reason
		}
		if gotDetail != tc.detail {
			t.Fatalf("finish %q detail = %q, want %q", tc.finish, gotDetail, tc.detail)
		}
	}
}

func TestSSESinkEmitsIncompleteFrameForTruncatedStream(t *testing.T) {
	responseID, err := canonical.GenerateResponseID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	started, err := canonical.NewResponseStartedEvent(sseTestEventID(t, 9), responseID, 0, "model")
	if err != nil {
		t.Fatal(err)
	}
	completed, err := canonical.NewResponseCompletedEvent(sseTestEventID(t, 10), responseID, 1, canonical.FinishReason("length"), canonical.Usage{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := NewEncoder(time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	writer := &flushBuffer{}
	sink, err := NewSSESink(writer, encoder)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Send(context.Background(), started); err != nil {
		t.Fatal(err)
	}
	if err := sink.Send(context.Background(), completed); err != nil {
		t.Fatal(err)
	}
	output := writer.String()
	if !bytes.Contains([]byte(output), []byte("event: response.incomplete")) {
		t.Fatalf("truncated stream emitted no response.incomplete frame: %s", output)
	}
	if bytes.Contains([]byte(output), []byte("event: response.completed")) {
		t.Fatalf("truncated stream emitted response.completed: %s", output)
	}
}
