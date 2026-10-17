package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// EncodeRequest maps one canonical request to a Chat Completions body.
// upstreamModel is the configured upstream model name the endpoint expects;
// the canonical request carries only the public alias. The stream flag
// controls the stream and stream_options fields.
func EncodeRequest(request canonical.Request, upstreamModel string, stream bool) ([]byte, error) {
	body, _, err := encodeRequestInternal(request, upstreamModel, stream)
	return body, err
}

// EncodeRequestWithTools maps one canonical request to a Chat Completions body
// and returns a ToolNameMap for response-side name resolution.
func EncodeRequestWithTools(request canonical.Request, upstreamModel string, stream bool) ([]byte, ToolNameMap, error) {
	return encodeRequestInternal(request, upstreamModel, stream)
}

func encodeRequestInternal(request canonical.Request, upstreamModel string, stream bool) ([]byte, ToolNameMap, error) {
	if !request.Valid() {
		return nil, nil, invalid("request", "canonical request is invalid")
	}
	if strings.TrimSpace(upstreamModel) == "" || strings.TrimSpace(upstreamModel) != upstreamModel {
		return nil, nil, invalid("request.upstream_model", "upstream model is invalid")
	}
	if request.Continuation() != nil {
		return nil, nil, unsupported("request.continuation")
	}
	messages, err := encodeMessages(request.Input())
	if err != nil {
		return nil, nil, err
	}
	tools, nameMap, err := encodeToolsWithMap(request.Tools())
	if err != nil {
		return nil, nil, err
	}
	body := map[string]any{
		"model":    upstreamModel,
		"messages": messages,
		"stream":   stream,
	}
	if stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = encodeToolChoice(request.ToolChoice())
		body["parallel_tool_calls"] = request.Output().ParallelToolCalls()
	}
	if output := request.Output(); output.Format() != canonical.OutputFormat("text") {
		body["response_format"] = encodeOutputFormat(output)
	}
	if value, ok := request.Sampling().Temperature(); ok {
		body["temperature"] = value
	}
	if value, ok := request.Sampling().TopP(); ok {
		body["top_p"] = value
	}
	if value, ok := request.Sampling().Seed(); ok {
		body["seed"] = value
	}
	if maxTokens := request.Output().MaxTokens(); maxTokens > 0 {
		body["max_tokens"] = maxTokens
	}
	encoded, err := json.Marshal(body)
	return encoded, nameMap, err
}

func encodeMessages(items []canonical.Item) ([]map[string]any, error) {
	messages := make([]map[string]any, 0, len(items))
	for index, item := range items {
		path := messagePath(index)
		switch item.Type() {
		case canonical.ItemType("message"):
			encoded, err := encodeMessageItem(item, path)
			if err != nil {
				return nil, err
			}
			messages = append(messages, encoded)
		case canonical.ItemType("tool_call"):
			call, ok := item.ToolCall()
			if !ok || call.Namespace() != "" {
				return nil, unsupported(path)
			}
			encodedCall := map[string]any{
				"id":   string(call.CallID()),
				"type": "function",
				"function": map[string]any{
					"name":      call.Name(),
					"arguments": string(call.Arguments().Bytes()),
				},
			}
			// Chat Completions requires every tool message to directly follow
			// the assistant turn that issued its call, so adjacent calls (and
			// the assistant text that precedes them) share one assistant turn.
			if last := len(messages) - 1; last >= 0 && messages[last]["role"] == "assistant" {
				existing, _ := messages[last]["tool_calls"].([]map[string]any)
				messages[last]["tool_calls"] = append(existing, encodedCall)
				continue
			}
			messages = append(messages, map[string]any{
				"role":       "assistant",
				"content":    nil,
				"tool_calls": []map[string]any{encodedCall},
			})
		case canonical.ItemType("tool_result"):
			result, ok := item.ToolResult()
			if !ok {
				return nil, unsupported(path)
			}
			text, err := blocksToText(result.Content(), path+".content")
			if err != nil {
				return nil, err
			}
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": string(result.CallID()),
				"content":      text,
			})
		default:
			return nil, unsupported(path)
		}
	}
	return messages, nil
}

func encodeMessageItem(item canonical.Item, path string) (map[string]any, error) {
	message, ok := item.Message()
	if !ok {
		return nil, unsupported(path)
	}
	text, err := blocksToText(message.Content(), path+".content")
	if err != nil {
		return nil, err
	}
	role := "user"
	switch message.Role() {
	case canonical.Role("system"), canonical.Role("developer"):
		role = "system"
	case canonical.Role("user"):
		role = "user"
	case canonical.Role("assistant"):
		role = "assistant"
	default:
		return nil, invalid(path+".role", "message role is unsupported")
	}
	return map[string]any{"role": role, "content": text}, nil
}

func blocksToText(blocks []canonical.ContentBlock, path string) (string, error) {
	text := ""
	for index, block := range blocks {
		blockPath := fmt.Sprintf("%s[%d]", path, index)
		if value, ok := block.Text(); ok {
			if text != "" {
				text += "\n"
			}
			text += value.Text()
			continue
		}
		if value, ok := block.JSON(); ok {
			if text != "" {
				text += "\n"
			}
			text += string(value.Value().Bytes())
			continue
		}
		return "", unsupported(blockPath)
	}
	return text, nil
}

func encodeToolChoice(choice canonical.ToolChoice) any {
	switch choice.Kind() {
	case "auto":
		return "auto"
	case "none":
		return "none"
	case "required":
		return "required"
	case "specific":
		return map[string]any{"type": "function", "function": map[string]any{"name": choice.Name()}}
	default:
		return "auto"
	}
}

func encodeOutputFormat(output canonical.OutputConstraints) map[string]any {
	switch output.Format() {
	case canonical.OutputFormat("json_object"):
		return map[string]any{"type": "json_object"}
	case canonical.OutputFormat("json_schema"):
		return map[string]any{"type": "json_schema", "json_schema": map[string]any{
			"name":   output.SchemaName(),
			"schema": json.RawMessage(output.Schema().Bytes()),
			"strict": output.Enforcement() == canonical.SchemaEnforcementStrict,
		}}
	default:
		return map[string]any{"type": "text"}
	}
}

func messagePath(index int) string {
	return fmt.Sprintf("request.input[%d]", index)
}
