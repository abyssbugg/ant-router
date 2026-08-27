package streaming

import (
	"bytes"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func streamEventID(t *testing.T, value byte) canonical.EventID {
	t.Helper()
	id, err := canonical.GenerateEventID(bytes.NewReader(bytes.Repeat([]byte{value}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatalf("GenerateEventID: %v", err)
	}
	return id
}

func streamItemID(t *testing.T, value byte) canonical.ItemID {
	t.Helper()
	id, err := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{value}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatalf("GenerateItemID: %v", err)
	}
	return id
}

func streamCallID(t *testing.T, value byte) canonical.ToolCallID {
	t.Helper()
	id, err := canonical.GenerateToolCallID(bytes.NewReader(bytes.Repeat([]byte{value}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatalf("GenerateToolCallID: %v", err)
	}
	return id
}

func streamResponseID(t *testing.T, value byte) canonical.ResponseID {
	t.Helper()
	id, err := canonical.GenerateResponseID(bytes.NewReader(bytes.Repeat([]byte{value}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatalf("GenerateResponseID: %v", err)
	}
	return id
}

func simpleEvent(t *testing.T) canonical.Event {
	t.Helper()
	event, err := canonical.NewResponseStartedEvent(streamEventID(t, 1), streamResponseID(t, 1), 0, "model")
	if err != nil {
		t.Fatalf("NewResponseStartedEvent: %v", err)
	}
	return event
}

func simpleResponse(t *testing.T) canonical.Response {
	t.Helper()
	response, err := canonical.NewResponse(canonical.ResponseParams{ID: streamResponseID(t, 1), Model: "model", FinishReason: canonical.FinishReason("content_filter"), Usage: canonical.Usage{}})
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	return response
}

func simpleFailure(t *testing.T) canonical.Failure {
	t.Helper()
	failure, err := canonical.NewFailure(canonical.FailureParams{Code: canonical.FailureCode("internal_error"), Message: "internal error", Retryable: true})
	if err != nil {
		t.Fatalf("NewFailure: %v", err)
	}
	return failure
}
