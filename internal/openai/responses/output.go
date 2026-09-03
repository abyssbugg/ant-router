package responses

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// Encoder maps canonical responses and events to OpenAI Responses wire values.
type Encoder struct {
	createdAt int64
}

// NewEncoder constructs a deterministic encoder using the supplied acceptance time.
func NewEncoder(createdAt time.Time) (Encoder, error) {
	seconds := createdAt.Unix()
	if seconds < 0 {
		return Encoder{}, newError(CodeInternal, "created_at", errors.New("response timestamp is invalid"))
	}
	return Encoder{createdAt: seconds}, nil
}

// EncodeResponse returns one completed OpenAI Responses JSON object.
func (e Encoder) EncodeResponse(response canonical.Response) ([]byte, error) {
	if !response.Valid() {
		return nil, newError(CodeInternal, "response", errors.New("canonical response is invalid"))
	}
	reasoningEntries, err := responseReasoningEntries(response)
	if err != nil {
		return nil, newError(CodeInternal, "response.continuation", errors.New("response continuation is invalid"))
	}
	output := make([]any, 0, len(response.Items()))
	for _, item := range response.Items() {
		value, err := encodeOutputItem(item, reasoningEntries)
		if err != nil {
			return nil, newError(CodeInternal, "response.output", errors.New("canonical output is invalid"))
		}
		output = append(output, value)
	}
	wire := wireResponse{
		ID: wireResponseID(response.ID()), Object: "response", CreatedAt: e.createdAt,
		Status: "completed", Model: response.Model(), Output: output, Usage: encodeUsage(response.Usage()),
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return nil, newError(CodeInternal, "response", errors.New("response encoding failed"))
	}
	return encoded, nil
}

type wireResponse struct {
	ID                string    `json:"id"`
	Object            string    `json:"object"`
	CreatedAt         int64     `json:"created_at"`
	Status            string    `json:"status"`
	Error             any       `json:"error"`
	IncompleteDetails any       `json:"incomplete_details"`
	Model             string    `json:"model"`
	Output            []any     `json:"output"`
	Usage             wireUsage `json:"usage"`
}

type wireUsage struct {
	InputTokens        int64                  `json:"input_tokens"`
	InputTokenDetails  wireInputTokenDetails  `json:"input_tokens_details"`
	OutputTokens       int64                  `json:"output_tokens"`
	OutputTokenDetails wireOutputTokenDetails `json:"output_tokens_details"`
	TotalTokens        int64                  `json:"total_tokens"`
}

type wireInputTokenDetails struct {
	CachedTokens int64 `json:"cached_tokens"`
}

type wireOutputTokenDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

type wireMessage struct {
	ID      string           `json:"id"`
	Type    string           `json:"type"`
	Status  string           `json:"status"`
	Role    string           `json:"role"`
	Content []wireOutputText `json:"content"`
}

type wireOutputText struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
}

type wireFunctionCall struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireReasoning struct {
	ID               string                 `json:"id"`
	Type             string                 `json:"type"`
	Status           string                 `json:"status"`
	Summary          []wireReasoningSummary `json:"summary"`
	EncryptedContent string                 `json:"encrypted_content,omitempty"`
}

type wireReasoningSummary struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func encodeOutputItem(item canonical.Item, reasoningEntries reasoningOutputEntries) (any, error) {
	switch item.Type() {
	case canonical.ItemType("message"):
		message, ok := item.Message()
		if !ok || message.Role() != canonical.Role("assistant") {
			return nil, errors.New("message is invalid")
		}
		content := make([]wireOutputText, 0, len(message.Content()))
		for _, block := range message.Content() {
			text := ""
			if value, ok := block.Text(); ok {
				text = value.Text()
			} else if value, ok := block.JSON(); ok {
				text = string(value.Value().Bytes())
			} else {
				return nil, errors.New("message content is unsupported")
			}
			content = append(content, wireOutputText{Type: "output_text", Text: text, Annotations: []any{}})
		}
		return wireMessage{ID: wireItemID("msg", item.ID()), Type: "message", Status: "completed", Role: "assistant", Content: content}, nil
	case canonical.ItemType("tool_call"):
		call, ok := item.ToolCall()
		if !ok {
			return nil, errors.New("function call is invalid")
		}
		return wireFunctionCall{ID: wireItemID("fc", item.ID()), Type: "function_call", Status: "completed", CallID: wireCallID(call.CallID()), Name: call.Name(), Arguments: string(call.Arguments().Bytes())}, nil
	case canonical.ItemType("reasoning_summary"):
		reasoning, ok := item.ReasoningSummary()
		if !ok {
			return nil, errors.New("reasoning summary is invalid")
		}
		summary := make([]wireReasoningSummary, 0, len(reasoning.Summaries()))
		for _, value := range reasoning.Summaries() {
			summary = append(summary, wireReasoningSummary{Type: "summary_text", Text: value.Text()})
		}
		wireID, encrypted := reasoningWireIdentity(item.ID(), reasoningEntries)
		return wireReasoning{ID: wireID, Type: "reasoning", Status: "completed", Summary: summary, EncryptedContent: encrypted}, nil
	default:
		return nil, errors.New("output item is unsupported")
	}
}

func encodeUsage(usage canonical.Usage) wireUsage {
	return wireUsage{
		InputTokens: usage.InputTokens(), InputTokenDetails: wireInputTokenDetails{CachedTokens: usage.CacheReadTokens()},
		OutputTokens: usage.OutputTokens(), OutputTokenDetails: wireOutputTokenDetails{ReasoningTokens: usage.ReasoningTokens()}, TotalTokens: usage.TotalTokens(),
	}
}

func wireResponseID(id canonical.ResponseID) string        { return "resp_" + string(id) }
func wireItemID(prefix string, id canonical.ItemID) string { return prefix + "_" + string(id) }
func wireCallID(id canonical.ToolCallID) string            { return "call_" + string(id) }
