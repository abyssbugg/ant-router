package canonical

import (
	"encoding/json"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func encodeItem(item Item) (json.RawMessage, error) {
	if !item.Valid() {
		return nil, codecError(CodeInvalidInvariant, "item")
	}
	switch item.Type() {
	case ItemType("message"):
		value, _ := item.Message()
		content := make([]json.RawMessage, 0, len(value.Content()))
		for _, block := range value.Content() {
			encoded, err := encodeContent(block)
			if err != nil {
				return nil, err
			}
			content = append(content, encoded)
		}
		return marshalCodec(messageDTO{Type: "message", ID: value.ID().String(), Role: string(value.Role()), Content: content}, MaxRequestBytes)
	case ItemType("tool_call"):
		value, _ := item.ToolCall()
		return marshalCodec(toolCallDTO{Type: "tool_call", ID: value.ID().String(), CallID: value.CallID().String(), Namespace: value.Namespace(), Name: value.Name(), Arguments: json.RawMessage(value.Arguments().Bytes())}, MaxRequestBytes)
	case ItemType("tool_result"):
		value, _ := item.ToolResult()
		content := make([]json.RawMessage, 0, len(value.Content()))
		for _, block := range value.Content() {
			encoded, err := encodeContent(block)
			if err != nil {
				return nil, err
			}
			content = append(content, encoded)
		}
		return marshalCodec(toolResultDTO{Type: "tool_result", ID: value.ID().String(), CallID: value.CallID().String(), Content: content, IsError: value.IsError()}, MaxRequestBytes)
	case ItemType("reasoning_summary"):
		value, _ := item.ReasoningSummary()
		summaries := make([]string, 0, len(value.Summaries()))
		for _, summary := range value.Summaries() {
			summaries = append(summaries, summary.Text())
		}
		return marshalCodec(reasoningDTO{Type: "reasoning_summary", ID: value.ID().String(), Summaries: summaries}, MaxRequestBytes)
	default:
		return nil, codecError(CodeInvalidVariant, "item.type")
	}
}

func decodeItem(data []byte) (Item, error) {
	kind, err := discriminator(data)
	if err != nil {
		return Item{}, err
	}
	switch kind {
	case "message":
		var dto messageDTO
		if err := strictDecode(data, &dto); err != nil {
			return Item{}, err
		}
		id, err := ParseItemID(dto.ID)
		if err != nil {
			return Item{}, err
		}
		content := make([]ContentBlock, 0, len(dto.Content))
		for _, raw := range dto.Content {
			block, err := decodeContent(raw)
			if err != nil {
				return Item{}, err
			}
			content = append(content, block)
		}
		value, err := NewMessage(id, Role(dto.Role), content)
		if err != nil {
			return Item{}, err
		}
		return NewMessageItem(value)
	case "tool_call":
		var dto toolCallDTO
		if err := strictDecode(data, &dto); err != nil {
			return Item{}, err
		}
		id, err := ParseItemID(dto.ID)
		if err != nil {
			return Item{}, err
		}
		callID, err := ParseToolCallID(dto.CallID)
		if err != nil {
			return Item{}, err
		}
		arguments, err := canonicaljson.Parse(dto.Arguments, canonicaljson.Limits{MaximumBytes: MaxJSONBytes, MaximumDepth: MaxJSONDepth})
		if err != nil {
			return Item{}, codecError(CodeInvalidJSON, "tool_call.arguments")
		}
		var value ToolCall
		if dto.Namespace == "" {
			value, err = NewToolCall(id, callID, dto.Name, arguments)
		} else {
			value, err = NewNamespacedToolCall(id, callID, dto.Namespace, dto.Name, arguments)
		}
		if err != nil {
			return Item{}, err
		}
		return NewToolCallItem(value)
	case "tool_result":
		var dto toolResultDTO
		if err := strictDecode(data, &dto); err != nil {
			return Item{}, err
		}
		id, err := ParseItemID(dto.ID)
		if err != nil {
			return Item{}, err
		}
		callID, err := ParseToolCallID(dto.CallID)
		if err != nil {
			return Item{}, err
		}
		content := make([]ContentBlock, 0, len(dto.Content))
		for _, raw := range dto.Content {
			block, err := decodeContent(raw)
			if err != nil {
				return Item{}, err
			}
			content = append(content, block)
		}
		value, err := NewToolResult(id, callID, content, dto.IsError)
		if err != nil {
			return Item{}, err
		}
		return NewToolResultItem(value)
	case "reasoning_summary":
		var dto reasoningDTO
		if err := strictDecode(data, &dto); err != nil {
			return Item{}, err
		}
		id, err := ParseItemID(dto.ID)
		if err != nil {
			return Item{}, err
		}
		summaries := make([]TextContent, 0, len(dto.Summaries))
		for _, text := range dto.Summaries {
			summary, err := NewTextContent(text)
			if err != nil {
				return Item{}, err
			}
			summaries = append(summaries, summary)
		}
		value, err := NewReasoningSummary(id, summaries)
		if err != nil {
			return Item{}, err
		}
		return NewReasoningSummaryItem(value)
	default:
		return Item{}, codecError(CodeInvalidVariant, "item.type")
	}
}
