package responses

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func decodeInput(raw json.RawMessage, ids IDGenerator, tools []canonical.ToolDefinition) ([]canonical.Item, error) {
	if len(raw) == 0 {
		return nil, newError(CodeInvalidField, "input", errors.New("input is required"))
	}
	if len(bytes.TrimSpace(raw)) > 0 && bytes.TrimSpace(raw)[0] == '"' {
		text, err := decodeStringInput(raw)
		if err != nil {
			return nil, err
		}
		item, err := messageItem(ids, canonical.Role("user"), text, "input")
		if err != nil {
			return nil, err
		}
		return []canonical.Item{item}, nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || len(values) == 0 || len(values) > canonical.MaxInputItems {
		return nil, newError(CodeInvalidField, "input", errors.New("input array is invalid"))
	}
	items := make([]canonical.Item, 0, len(values))
	calls := make(map[string]canonical.ToolCallID)
	results := make(map[string]struct{})
	toolNames := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		toolNames[tool.Name()] = struct{}{}
	}
	for index, value := range values {
		path := fmt.Sprintf("input[%d]", index)
		fields, err := strictObject(value, path, "type", "role", "content", "call_id", "name", "arguments", "output")
		if err != nil {
			return nil, err
		}
		kind := ""
		if _, hasType := fields["type"]; hasType {
			kind, err = requiredString(fields, "type", path+".type")
			if err != nil {
				return nil, err
			}
		} else if _, hasRole := fields["role"]; hasRole {
			if _, hasContent := fields["content"]; hasContent {
				kind = "message"
			}
		}
		if kind == "" {
			return nil, newError(CodeInvalidField, path+".type", errors.New("item type is required"))
		}
		var item canonical.Item
		switch kind {
		case "message":
			item, err = decodeMessage(fields, ids, path)
		case "function_call":
			item, err = decodeFunctionCall(fields, ids, calls, toolNames, path)
		case "function_call_output":
			item, err = decodeFunctionOutput(fields, ids, calls, results, path)
		default:
			return nil, newError(CodeUnsupportedField, path+".type", errors.New("input item type is unsupported"))
		}
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func decodeMessage(fields map[string]json.RawMessage, ids IDGenerator, path string) (canonical.Item, error) {
	if err := rejectFields(fields, path, "type", "role", "content"); err != nil {
		return canonical.Item{}, err
	}
	roleValue, err := requiredString(fields, "role", path+".role")
	if err != nil {
		return canonical.Item{}, err
	}
	role := canonical.Role(roleValue)
	if !role.Valid() {
		return canonical.Item{}, newError(CodeInvalidField, path+".role", errors.New("role is invalid"))
	}
	rawContent, ok := fields["content"]
	if !ok {
		return canonical.Item{}, newError(CodeInvalidField, path+".content", errors.New("content is required"))
	}
	blocks := make([]canonical.ContentBlock, 0, 1)
	trimmed := bytes.TrimSpace(rawContent)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if json.Unmarshal(rawContent, &text) != nil {
			return canonical.Item{}, newError(CodeInvalidField, path+".content", errors.New("content is invalid"))
		}
		block, err := responseTextBlock(text, path+".content")
		if err != nil {
			return canonical.Item{}, err
		}
		blocks = append(blocks, block)
	} else {
		var rawBlocks []json.RawMessage
		if err := json.Unmarshal(rawContent, &rawBlocks); err != nil || len(rawBlocks) == 0 || len(rawBlocks) > canonical.MaxContentBlocks {
			return canonical.Item{}, newError(CodeInvalidField, path+".content", errors.New("content is invalid"))
		}
		blocks = make([]canonical.ContentBlock, 0, len(rawBlocks))
		for index, rawBlock := range rawBlocks {
			blockPath := fmt.Sprintf("%s.content[%d]", path, index)
			blockFields, err := strictObject(rawBlock, blockPath, "type", "text", "image_url", "file_id", "detail")
			if err != nil {
				return canonical.Item{}, err
			}
			kind, err := requiredString(blockFields, "type", blockPath+".type")
			if err != nil {
				return canonical.Item{}, err
			}
			if kind == "input_image" {
				return canonical.Item{}, newError(CodeUnsupportedField, blockPath+".type", errors.New("image input is deferred"))
			}
			if kind != "input_text" && kind != "output_text" {
				return canonical.Item{}, newError(CodeUnsupportedField, blockPath+".type", errors.New("content type is unsupported"))
			}
			if err := rejectFields(blockFields, blockPath, "type", "text"); err != nil {
				return canonical.Item{}, err
			}
			text, err := requiredString(blockFields, "text", blockPath+".text")
			if err != nil {
				return canonical.Item{}, err
			}
			block, err := responseTextBlock(text, blockPath+".text")
			if err != nil {
				return canonical.Item{}, err
			}
			blocks = append(blocks, block)
		}
	}
	itemID, err := ids.ItemID()
	if err != nil {
		return canonical.Item{}, newError(CodeInternal, path+".id", errors.New("ID generation failed"))
	}
	message, err := canonical.NewMessage(itemID, role, blocks)
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path, errors.New("message is invalid"))
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path, errors.New("message is invalid"))
	}
	return item, nil
}

func responseTextBlock(text, path string) (canonical.ContentBlock, error) {
	if text == "" {
		return canonical.ContentBlock{}, newError(CodeInvalidField, path, errors.New("text is invalid"))
	}
	value, err := canonical.NewTextContent(text)
	if err != nil {
		return canonical.ContentBlock{}, newError(CodeInvalidField, path, errors.New("text is invalid"))
	}
	block, err := canonical.NewTextBlock(value)
	if err != nil {
		return canonical.ContentBlock{}, newError(CodeInvalidField, path, errors.New("text is invalid"))
	}
	return block, nil
}

func decodeFunctionCall(fields map[string]json.RawMessage, ids IDGenerator, calls map[string]canonical.ToolCallID, toolNames map[string]struct{}, path string) (canonical.Item, error) {
	if err := rejectFields(fields, path, "type", "call_id", "name", "arguments"); err != nil {
		return canonical.Item{}, err
	}
	externalID, err := requiredString(fields, "call_id", path+".call_id")
	if err != nil || externalID == "" {
		return canonical.Item{}, newError(CodeInvalidField, path+".call_id", errors.New("call ID is invalid"))
	}
	if _, exists := calls[externalID]; exists {
		return canonical.Item{}, newError(CodeInvalidField, path+".call_id", errors.New("call ID is duplicated"))
	}
	name, err := requiredString(fields, "name", path+".name")
	if err != nil {
		return canonical.Item{}, err
	}
	if _, declared := toolNames[name]; !declared {
		return canonical.Item{}, newError(CodeInvalidField, path+".name", errors.New("tool is not declared"))
	}
	argumentsText, err := requiredString(fields, "arguments", path+".arguments")
	if err != nil {
		return canonical.Item{}, err
	}
	arguments, err := canonicaljson.Parse([]byte(argumentsText), canonicaljson.DefaultLimits())
	if err != nil || !arguments.IsObject() {
		return canonical.Item{}, newError(CodeInvalidField, path+".arguments", errors.New("arguments are invalid"))
	}
	itemID, err := ids.ItemID()
	if err != nil {
		return canonical.Item{}, newError(CodeInternal, path+".id", errors.New("ID generation failed"))
	}
	callID, err := ids.ToolCallID()
	if err != nil {
		return canonical.Item{}, newError(CodeInternal, path+".call_id", errors.New("ID generation failed"))
	}
	call, err := canonical.NewToolCall(itemID, callID, name, arguments)
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path, errors.New("function call is invalid"))
	}
	item, err := canonical.NewToolCallItem(call)
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path, errors.New("function call is invalid"))
	}
	calls[externalID] = callID
	return item, nil
}

func decodeFunctionOutput(fields map[string]json.RawMessage, ids IDGenerator, calls map[string]canonical.ToolCallID, results map[string]struct{}, path string) (canonical.Item, error) {
	if err := rejectFields(fields, path, "type", "call_id", "output"); err != nil {
		return canonical.Item{}, err
	}
	externalID, err := requiredString(fields, "call_id", path+".call_id")
	if err != nil {
		return canonical.Item{}, err
	}
	callID, exists := calls[externalID]
	if !exists {
		return canonical.Item{}, newError(CodeInvalidField, path+".call_id", errors.New("call ID does not reference a prior call"))
	}
	if _, exists := results[externalID]; exists {
		return canonical.Item{}, newError(CodeInvalidField, path+".call_id", errors.New("call output is duplicated"))
	}
	output, err := requiredString(fields, "output", path+".output")
	if err != nil || output == "" {
		return canonical.Item{}, newError(CodeInvalidField, path+".output", errors.New("output is invalid"))
	}
	text, err := canonical.NewTextContent(output)
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path+".output", errors.New("output is invalid"))
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path+".output", errors.New("output is invalid"))
	}
	itemID, err := ids.ItemID()
	if err != nil {
		return canonical.Item{}, newError(CodeInternal, path+".id", errors.New("ID generation failed"))
	}
	result, err := canonical.NewToolResult(itemID, callID, []canonical.ContentBlock{block}, false)
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path, errors.New("function output is invalid"))
	}
	item, err := canonical.NewToolResultItem(result)
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path, errors.New("function output is invalid"))
	}
	results[externalID] = struct{}{}
	return item, nil
}

func strictObject(raw json.RawMessage, path string, allowed ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, newError(CodeInvalidField, path, errors.New("object is invalid"))
	}
	if err := rejectFields(fields, path, allowed...); err != nil {
		return nil, err
	}
	return fields, nil
}

func rejectFields(fields map[string]json.RawMessage, path string, allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		set[field] = struct{}{}
	}
	unknown := make([]string, 0)
	for field := range fields {
		if _, ok := set[field]; !ok {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return newError(CodeUnknownField, path+"."+unknown[0], errors.New("field is unknown"))
}

func requiredString(fields map[string]json.RawMessage, field, path string) (string, error) {
	raw, ok := fields[field]
	if !ok {
		return "", newError(CodeInvalidField, path, errors.New("field is required"))
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", newError(CodeInvalidField, path, errors.New("field type is invalid"))
	}
	return value, nil
}
