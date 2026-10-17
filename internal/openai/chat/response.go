package chat

import (
	"encoding/json"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// IDGenerator supplies canonical identifiers so callers can inject
// deterministic generation in tests.
type IDGenerator interface {
	ItemID() (canonical.ItemID, error)
	ToolCallID() (canonical.ToolCallID, error)
	EventID() (canonical.EventID, error)
}

// Chat request/response wire shapes. Unknown upstream fields are tolerated:
// compatible endpoints routinely add vendor extensions.
type ToolFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCallWire struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function ToolFn `json:"function"`
}

type MessageWire struct {
	Role             string         `json:"role"`
	Content          *string        `json:"content"`
	ReasoningContent *string        `json:"reasoning_content"`
	ToolCalls        []ToolCallWire `json:"tool_calls"`
}

type UsageDetails struct {
	CachedTokens int64 `json:"cached_tokens"`
}

type UsageWire struct {
	PromptTokens            int64        `json:"prompt_tokens"`
	CompletionTokens        int64        `json:"completion_tokens"`
	TotalTokens             int64        `json:"total_tokens"`
	PromptTokensDetails     UsageDetails `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type ChoiceWire struct {
	Message      MessageWire `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// Completion is one non-streaming Chat Completions response.
type Completion struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []ChoiceWire `json:"choices"`
	Usage   *UsageWire   `json:"usage"`
	Error   *ErrorWire   `json:"error"`
}

type ErrorWire struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// DecodeCompletion parses one non-streaming chat response body.
func DecodeCompletion(data []byte) (Completion, error) {
	var completion Completion
	if err := json.Unmarshal(data, &completion); err != nil {
		return Completion{}, protocolErr("response.json")
	}
	if completion.Error != nil {
		return Completion{}, &Error{Code: "upstream_error", Path: "response.error", Message: "upstream returned an error response"}
	}
	if len(completion.Choices) != 1 {
		return Completion{}, protocolErr("response.choices")
	}
	return completion, nil
}

// UsageParams converts wire usage counters into canonical usage parameters.
// Vendors must report self-consistent totals; canonical rejects anything else.
func UsageParams(usage UsageWire) canonical.UsageParams {
	return canonical.UsageParams{
		InputTokens:     usage.PromptTokens,
		OutputTokens:    usage.CompletionTokens,
		ReasoningTokens: usage.CompletionTokensDetails.ReasoningTokens,
		CacheReadTokens: usage.PromptTokensDetails.CachedTokens,
		TotalTokens:     usage.TotalTokens,
	}
}

// BuildResponse assembles the canonical response for one non-streaming chat
// completion. nameMap maps upstream function names back to the canonical
// tools they were encoded from; nil means every call is a plain function.
func BuildResponse(responseID canonical.ResponseID, model string, completion Completion, ids IDGenerator, nameMap ToolNameMap) (canonical.Response, error) {
	if len(completion.Choices) != 1 {
		return canonical.Response{}, protocolErr("response.choices")
	}
	choice := completion.Choices[0]
	var items []canonical.Item
	if choice.Message.ReasoningContent != nil && *choice.Message.ReasoningContent != "" {
		summary, err := canonical.NewTextContent(*choice.Message.ReasoningContent)
		if err != nil {
			return canonical.Response{}, protocolErr("response.reasoning_content")
		}
		itemID, err := ids.ItemID()
		if err != nil {
			return canonical.Response{}, protocolErr("response.item_id")
		}
		reasoning, err := canonical.NewReasoningSummary(itemID, []canonical.TextContent{summary})
		if err != nil {
			return canonical.Response{}, protocolErr("response.reasoning_summary")
		}
		item, err := canonical.NewReasoningSummaryItem(reasoning)
		if err != nil {
			return canonical.Response{}, protocolErr("response.reasoning_item")
		}
		items = append(items, item)
	}
	if choice.Message.Content != nil && *choice.Message.Content != "" {
		text, err := canonical.NewTextContent(*choice.Message.Content)
		if err != nil {
			return canonical.Response{}, protocolErr("response.content")
		}
		block, err := canonical.NewTextBlock(text)
		if err != nil {
			return canonical.Response{}, protocolErr("response.content")
		}
		itemID, err := ids.ItemID()
		if err != nil {
			return canonical.Response{}, protocolErr("response.item_id")
		}
		message, err := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
		if err != nil {
			return canonical.Response{}, protocolErr("response.message")
		}
		item, err := canonical.NewMessageItem(message)
		if err != nil {
			return canonical.Response{}, protocolErr("response.item")
		}
		items = append(items, item)
	}
	for _, call := range choice.Message.ToolCalls {
		if call.Type != "" && call.Type != "function" {
			return canonical.Response{}, protocolErr("response.tool_calls.type")
		}
		itemID, err := ids.ItemID()
		if err != nil {
			return canonical.Response{}, protocolErr("response.item_id")
		}
		callID, err := ids.ToolCallID()
		if err != nil {
			return canonical.Response{}, protocolErr("response.call_id")
		}
		item, err := buildToolCallItem(resolveTool(nameMap, call.Function.Name), itemID, callID, call.Function.Arguments)
		if err != nil {
			return canonical.Response{}, err
		}
		items = append(items, item)
	}
	usage := canonical.Usage{}
	if completion.Usage != nil {
		value, err := canonical.NewUsage(UsageParams(*completion.Usage))
		if err != nil {
			return canonical.Response{}, protocolErr("response.usage")
		}
		usage = value
	}
	return canonical.NewResponse(canonical.ResponseParams{
		ID: responseID, Model: model, Items: items,
		FinishReason: FinishReason(choice.FinishReason), Usage: usage,
	})
}

func FinishReason(raw string) canonical.FinishReason {
	switch raw {
	case "stop":
		return canonical.FinishReason("stop")
	case "length":
		return canonical.FinishReason("length")
	case "tool_calls", "function_call":
		return canonical.FinishReason("tool_call")
	case "content_filter":
		return canonical.FinishReason("content_filter")
	default:
		return canonical.FinishReason("other")
	}
}
