package anthropic

import (
	"bytes"
	"encoding/base64"
	"encoding/json"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

func mapSystemContent(blocks []canonical.ContentBlock, path string) ([]sdk.TextBlockParam, error) {
	result := make([]sdk.TextBlockParam, 0, len(blocks))
	for index, block := range blocks {
		text, err := contentText(block)
		if err != nil {
			return nil, requestMapError(path + "[" + jsonIndex(index) + "]")
		}
		result = append(result, sdk.TextBlockParam{Text: text})
	}
	return result, nil
}

func mapMessageContent(blocks []canonical.ContentBlock, role canonical.Role, path string) ([]sdk.ContentBlockParamUnion, error) {
	result := make([]sdk.ContentBlockParamUnion, 0, len(blocks))
	for index, block := range blocks {
		itemPath := path + "[" + jsonIndex(index) + "]"
		if text, err := contentText(block); err == nil {
			result = append(result, sdk.NewTextBlock(text))
			continue
		}
		image, ok := block.Image()
		if !ok || role != canonical.Role("user") || image.Detail() != canonical.ImageDetailAuto {
			return nil, requestMapError(itemPath)
		}
		mapped, err := mapImage(image)
		if err != nil {
			return nil, requestMapError(itemPath)
		}
		result = append(result, mapped)
	}
	return result, nil
}

func contentText(block canonical.ContentBlock) (string, error) {
	if text, ok := block.Text(); ok {
		return text.Text(), nil
	}
	if value, ok := block.JSON(); ok {
		return string(value.Value().Bytes()), nil
	}
	return "", requestMapError("content")
}

func mapImage(image canonical.ImageContent) (sdk.ContentBlockParamUnion, error) {
	if rawURL, ok := image.URL(); ok {
		return sdk.NewImageBlock(sdk.URLImageSourceParam{URL: rawURL}), nil
	}
	data, ok := image.Bytes()
	if !ok {
		return sdk.ContentBlockParamUnion{}, requestMapError("image")
	}
	return sdk.NewImageBlockBase64(image.MIME(), base64.StdEncoding.EncodeToString(data)), nil
}

func mapToolResult(result canonical.ToolResult, path string) (sdk.ContentBlockParamUnion, error) {
	content := make([]sdk.ToolResultBlockParamContentUnion, 0, len(result.Content()))
	for index, block := range result.Content() {
		itemPath := path + ".content[" + jsonIndex(index) + "]"
		if text, err := contentText(block); err == nil {
			content = append(content, sdk.ToolResultBlockParamContentUnion{OfText: &sdk.TextBlockParam{Text: text}})
			continue
		}
		image, ok := block.Image()
		if !ok || image.Detail() != canonical.ImageDetailAuto {
			return sdk.ContentBlockParamUnion{}, requestMapError(itemPath)
		}
		mapped, err := mapImage(image)
		if err != nil {
			return sdk.ContentBlockParamUnion{}, requestMapError(itemPath)
		}
		content = append(content, sdk.ToolResultBlockParamContentUnion{OfImage: mapped.OfImage})
	}
	block := &sdk.ToolResultBlockParam{ToolUseID: result.CallID().String(), IsError: param.NewOpt(result.IsError()), Content: content}
	return sdk.ContentBlockParamUnion{OfToolResult: block}, nil
}

func canonicalAny(value canonicaljson.Value) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(value.Bytes()))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return preserveJSONNumbers(result), nil
}

func preserveJSONNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		return json.RawMessage(typed.String())
	case map[string]any:
		for key, item := range typed {
			typed[key] = preserveJSONNumbers(item)
		}
	case []any:
		for index, item := range typed {
			typed[index] = preserveJSONNumbers(item)
		}
	}
	return value
}

func jsonIndex(value int) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[position:])
}
