package canonical

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ResponseParams contains completed canonical response fields.
type ResponseParams struct {
	ID           ResponseID
	Model        string
	Items        []Item
	FinishReason FinishReason
	Usage        Usage
	Continuation *Continuation
}

// Response is an immutable completed canonical response.
type Response struct {
	id           ResponseID
	model        string
	items        []Item
	finishReason FinishReason
	usage        Usage
	continuation *Continuation
}

// NewResponse validates and defensively copies a completed response.
func NewResponse(params ResponseParams) (Response, error) {
	var continuation *Continuation
	if params.Continuation != nil {
		copyValue := *params.Continuation
		continuation = &copyValue
	}
	response := Response{
		id:           params.ID,
		model:        params.Model,
		items:        append([]Item(nil), params.Items...),
		finishReason: params.FinishReason,
		usage:        params.Usage,
		continuation: continuation,
	}
	if err := response.validate(); err != nil {
		return Response{}, err
	}
	return response, nil
}

func (r Response) ID() ResponseID             { return r.id }
func (r Response) Model() string              { return r.model }
func (r Response) Items() []Item              { return append([]Item(nil), r.items...) }
func (r Response) FinishReason() FinishReason { return r.finishReason }
func (r Response) Usage() Usage               { return r.usage }
func (r Response) Continuation() (Continuation, bool) {
	if r.continuation == nil {
		return Continuation{}, false
	}
	return *r.continuation, true
}
func (r Response) WithContinuation(value Continuation) (Response, error) {
	return NewResponse(ResponseParams{ID: r.id, Model: r.model, Items: r.items, FinishReason: r.finishReason, Usage: r.usage, Continuation: &value})
}
func (r Response) Valid() bool { return r.validate() == nil }

func (r Response) validate() error {
	if !r.id.Valid() {
		return responseError(CodeInvalidID, "response.id")
	}
	if r.model == "" || strings.TrimSpace(r.model) != r.model || !utf8.ValidString(r.model) {
		return responseError(CodeInvalidInvariant, "response.model")
	}
	if len(r.model) > MaxModelNameBytes {
		return responseError(CodeLimitExceeded, "response.model")
	}
	if len(r.items) > MaxResponseItems {
		return responseError(CodeLimitExceeded, "response.items")
	}
	if !r.finishReason.Valid() || r.finishReason == FinishReason("cancelled") || r.finishReason == FinishReason("error") {
		return responseError(CodeInvalidInvariant, "response.finish_reason")
	}
	if !r.usage.Valid() {
		return responseError(CodeInvalidInvariant, "response.usage")
	}
	if r.continuation != nil && !r.continuation.Valid() {
		return responseError(CodeInvalidInvariant, "response.continuation")
	}
	itemIDs := make(map[ItemID]struct{}, len(r.items))
	callIDs := make(map[ToolCallID]struct{})
	hasToolCall := false
	for _, item := range r.items {
		if !item.Valid() || !item.ID().Valid() || item.Type() == ItemType("tool_result") {
			return responseError(CodeInvalidInvariant, "response.items")
		}
		if reasoning, ok := item.ReasoningSummary(); ok && len(reasoning.Summaries()) == 0 && r.continuation == nil {
			return responseError(CodeInvalidInvariant, "response.items")
		}
		if _, duplicate := itemIDs[item.ID()]; duplicate {
			return responseError(CodeInvalidInvariant, "response.items")
		}
		itemIDs[item.ID()] = struct{}{}
		var callID ToolCallID
		switch item.Type() {
		case ItemType("tool_call"):
			hasToolCall = true
			call, _ := item.ToolCall()
			callID = call.CallID()
		case ItemType("custom_tool_call"):
			hasToolCall = true
			call, _ := item.CustomToolCall()
			callID = call.CallID()
		}
		if callID.Valid() {
			if _, duplicate := callIDs[callID]; duplicate {
				return responseError(CodeInvalidInvariant, "response.items")
			}
			callIDs[callID] = struct{}{}
		}
	}
	if (r.finishReason == FinishReason("tool_call")) != hasToolCall {
		return responseError(CodeInvalidInvariant, "response.finish_reason")
	}
	return nil
}

func responseError(code ErrorCode, path string) error {
	return &Error{Code: code, Path: path, Err: errors.New("response is invalid")}
}
