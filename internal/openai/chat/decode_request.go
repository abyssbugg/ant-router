package chat

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

// Chat request wire shapes (inbound). Unknown upstream fields are tolerated:
// compatible clients routinely add vendor extensions.
type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type wireToolChoiceFunction struct {
	Name string `json:"name"`
}

type wireToolChoice struct {
	Type     string                 `json:"type"`
	Function wireToolChoiceFunction `json:"function"`
}

type wireResponseFormat struct {
	Type       string `json:"type"`
	JSONSchema struct {
		Name   string          `json:"name"`
		Schema json.RawMessage `json:"schema"`
		Strict bool            `json:"strict"`
	} `json:"json_schema"`
}

type wireMessage struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	ToolCallID       string          `json:"tool_call_id"`
	ToolCalls        []ToolCallWire  `json:"tool_calls"`
	ReasoningContent *string         `json:"reasoning_content"`
}

type wireRequest struct {
	Model             string              `json:"model"`
	Messages          []wireMessage       `json:"messages"`
	Tools             []wireTool          `json:"tools"`
	ToolChoice        json.RawMessage     `json:"tool_choice"`
	Temperature       *float64            `json:"temperature"`
	TopP              *float64            `json:"top_p"`
	Seed              *int64              `json:"seed"`
	MaxTokens         *int                `json:"max_tokens"`
	ResponseFormat    *wireResponseFormat `json:"response_format"`
	ParallelToolCalls *bool               `json:"parallel_tool_calls"`
	Stream            bool                `json:"stream"`
}

// DecodedRequest carries the canonical request plus the delivery mode the
// client selected.
type DecodedRequest struct {
	Request canonical.Request
	Stream  bool
}

// DecodeRequest maps one Chat Completions request body to the canonical
// protocol. requestID becomes the canonical request identity; ids supplies
// the remaining canonical identifiers. Wire identifiers are never trusted as
// canonical identity: tool results are linked to the canonical tool calls
// they reference through a wire-id translation table.
func DecodeRequest(data []byte, requestID canonical.RequestID, ids IDGenerator) (DecodedRequest, error) {
	var wire wireRequest
	if err := json.Unmarshal(data, &wire); err != nil {
		// A body the client itself malformed is a client fault, not an
		// upstream fault: classify it as invalid, never protocol.
		return DecodedRequest{}, invalid("request.json", "request body is not a valid chat completion request")
	}
	if strings.TrimSpace(wire.Model) == "" || strings.TrimSpace(wire.Model) != wire.Model {
		return DecodedRequest{}, invalid("request.model", "model is invalid")
	}
	if len(wire.Messages) == 0 {
		return DecodedRequest{}, invalid("request.messages", "at least one message is required")
	}
	decoder := &requestDecoder{ids: ids, callIDs: make(map[string]canonical.ToolCallID)}
	items := make([]canonical.Item, 0, len(wire.Messages))
	for index := range wire.Messages {
		mapped, err := decoder.decodeMessage(&wire.Messages[index], fmt.Sprintf("request.messages[%d]", index))
		if err != nil {
			return DecodedRequest{}, err
		}
		items = append(items, mapped...)
	}
	tools, err := decoder.decodeTools(wire.Tools)
	if err != nil {
		return DecodedRequest{}, err
	}
	toolChoice, err := decoder.decodeToolChoice(wire.ToolChoice)
	if err != nil {
		return DecodedRequest{}, err
	}
	output, err := decoder.decodeOutput(wire, len(tools))
	if err != nil {
		return DecodedRequest{}, err
	}
	var sampling canonical.Sampling
	if wire.Temperature != nil || wire.TopP != nil || wire.Seed != nil {
		sampling, err = canonical.NewSampling(canonical.SamplingParams{
			Temperature: wire.Temperature, TopP: wire.TopP, Seed: wire.Seed,
		})
		if err != nil {
			return DecodedRequest{}, invalid("request.sampling", "sampling parameters are invalid")
		}
	}
	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:         requestID,
		Model:      wire.Model,
		Input:      items,
		Tools:      tools,
		ToolChoice: toolChoice,
		Output:     output,
		Sampling:   sampling,
	})
	if err != nil {
		return DecodedRequest{}, translateCanonicalError(err)
	}
	return DecodedRequest{Request: request, Stream: wire.Stream}, nil
}

type requestDecoder struct {
	ids     IDGenerator
	callIDs map[string]canonical.ToolCallID // wire call id -> canonical ToolCallID
}

func (d *requestDecoder) decodeMessage(message *wireMessage, path string) ([]canonical.Item, error) {
	if message.ReasoningContent != nil {
		return nil, unsupported(path + ".reasoning_content")
	}
	switch message.Role {
	case "tool":
		return d.decodeToolResult(message, path)
	case "assistant":
		if len(message.ToolCalls) > 0 {
			items, err := d.decodeAssistantToolCalls(message, path)
			if err != nil {
				return nil, err
			}
			// Assistant text accompanying tool calls is preserved as its own
			// preceding assistant message rather than silently dropped.
			if len(message.Content) > 0 && string(message.Content) != "null" {
				blocks, err := d.decodeContent(message.Content, path+".content")
				if err != nil {
					return nil, err
				}
				if len(blocks) > 0 {
					textItemID, err := d.ids.ItemID()
					if err != nil {
						return nil, protocolErr("request.id")
					}
					textMessage, err := canonical.NewMessage(textItemID, canonical.Role("assistant"), blocks)
					if err != nil {
						return nil, translateCanonicalError(err)
					}
					textItem, err := canonical.NewMessageItem(textMessage)
					if err != nil {
						return nil, translateCanonicalError(err)
					}
					items = append([]canonical.Item{textItem}, items...)
				}
			}
			return items, nil
		}
	}
	blocks, err := d.decodeContent(message.Content, path+".content")
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, invalid(path+".content", "message content is empty")
	}
	for _, block := range blocks {
		if value, ok := block.Text(); ok && strings.TrimSpace(value.Text()) == "" {
			return nil, invalid(path+".content", "message content is empty")
		}
	}
	role := canonical.Role(message.Role)
	switch role {
	case canonical.Role("system"), canonical.Role("developer"), canonical.Role("user"), canonical.Role("assistant"):
	default:
		return nil, invalid(path+".role", "message role is unsupported")
	}
	itemID, err := d.ids.ItemID()
	if err != nil {
		return nil, protocolErr("request.id")
	}
	itemMessage, err := canonical.NewMessage(itemID, role, blocks)
	if err != nil {
		return nil, translateCanonicalError(err)
	}
	item, err := canonical.NewMessageItem(itemMessage)
	if err != nil {
		return nil, translateCanonicalError(err)
	}
	return []canonical.Item{item}, nil
}

func (d *requestDecoder) decodeAssistantToolCalls(message *wireMessage, path string) ([]canonical.Item, error) {
	items := make([]canonical.Item, 0, len(message.ToolCalls))
	for index, call := range message.ToolCalls {
		callPath := fmt.Sprintf("%s.tool_calls[%d]", path, index)
		if call.Type != "" && call.Type != "function" {
			return nil, unsupported(callPath + ".type")
		}
		arguments, err := canonicaljson.Parse([]byte(call.Function.Arguments), canonicaljson.DefaultLimits())
		if err != nil {
			return nil, invalid(callPath+".function.arguments", "tool arguments must be valid JSON")
		}
		itemID, err := d.ids.ItemID()
		if err != nil {
			return nil, protocolErr("request.id")
		}
		callID, err := d.ids.ToolCallID()
		if err != nil {
			return nil, protocolErr("request.id")
		}
		toolCall, err := canonical.NewToolCall(itemID, callID, call.Function.Name, arguments)
		if err != nil {
			return nil, translateCanonicalError(err)
		}
		item, err := canonical.NewToolCallItem(toolCall)
		if err != nil {
			return nil, translateCanonicalError(err)
		}
		if call.ID != "" {
			if _, duplicate := d.callIDs[call.ID]; duplicate {
				return nil, invalid(callPath+".id", "duplicate tool_call id")
			}
			d.callIDs[call.ID] = callID
		}
		items = append(items, item)
	}
	return items, nil
}

func (d *requestDecoder) decodeToolResult(message *wireMessage, path string) ([]canonical.Item, error) {
	if message.ToolCallID == "" {
		return nil, invalid(path+".tool_call_id", "tool result requires a tool_call_id")
	}
	callID, referenced := d.callIDs[message.ToolCallID]
	if !referenced {
		return nil, invalid(path+".tool_call_id", "tool result references an unknown tool call")
	}
	text, err := d.contentToText(message.Content, path+".content")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, invalid(path+".content", "tool result content is empty")
	}
	content, err := canonical.NewTextContent(text)
	if err != nil {
		return nil, translateCanonicalError(err)
	}
	block, err := canonical.NewTextBlock(content)
	if err != nil {
		return nil, translateCanonicalError(err)
	}
	itemID, err := d.ids.ItemID()
	if err != nil {
		return nil, protocolErr("request.id")
	}
	result, err := canonical.NewToolResult(itemID, callID, []canonical.ContentBlock{block}, false)
	if err != nil {
		return nil, translateCanonicalError(err)
	}
	item, err := canonical.NewToolResultItem(result)
	if err != nil {
		return nil, translateCanonicalError(err)
	}
	return []canonical.Item{item}, nil
}

func (d *requestDecoder) decodeContent(raw json.RawMessage, path string) ([]canonical.ContentBlock, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		content, err := canonical.NewTextContent(asString)
		if err != nil {
			return nil, translateCanonicalError(err)
		}
		block, err := canonical.NewTextBlock(content)
		if err != nil {
			return nil, translateCanonicalError(err)
		}
		return []canonical.ContentBlock{block}, nil
	}
	var parts []wireContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, invalid(path, "content must be a string or an array of parts")
	}
	blocks := make([]canonical.ContentBlock, 0, len(parts))
	for index := range parts {
		block, err := d.decodeContentPart(&parts[index], fmt.Sprintf("%s[%d]", path, index))
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func (d *requestDecoder) contentToText(raw json.RawMessage, path string) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}
	var parts []wireContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", invalid(path, "content must be a string or an array of parts")
	}
	text := ""
	for index := range parts {
		if parts[index].Type != "text" {
			return "", unsupported(fmt.Sprintf("%s[%d]", path, index))
		}
		if text != "" {
			text += "\n"
		}
		text += parts[index].Text
	}
	return text, nil
}

type wireContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL struct {
		URL    string `json:"url"`
		Detail string `json:"detail"`
	} `json:"image_url"`
}

func (d *requestDecoder) decodeContentPart(part *wireContentPart, path string) (canonical.ContentBlock, error) {
	switch part.Type {
	case "text":
		content, err := canonical.NewTextContent(part.Text)
		if err != nil {
			return canonical.ContentBlock{}, translateCanonicalError(err)
		}
		return canonical.NewTextBlock(content)
	case "image_url":
		return d.decodeImagePart(part, path)
	default:
		return canonical.ContentBlock{}, unsupported(path + ".type")
	}
}

func (d *requestDecoder) decodeImagePart(part *wireContentPart, path string) (canonical.ContentBlock, error) {
	url := part.ImageURL.URL
	detail := canonical.ImageDetailAuto
	if part.ImageURL.Detail != "" {
		detail = canonical.ImageDetail(part.ImageURL.Detail)
		if !detail.Valid() {
			return canonical.ContentBlock{}, invalid(path+".image_url.detail", "detail hint is invalid")
		}
	}
	if strings.HasPrefix(url, "data:") {
		rest := strings.TrimPrefix(url, "data:")
		separator := strings.Index(rest, ";base64,")
		if separator <= 0 {
			return canonical.ContentBlock{}, invalid(path+".image_url.url", "data URL must be data:<mime>;base64,<payload>")
		}
		mime := rest[:separator]
		payload, err := base64.StdEncoding.DecodeString(rest[separator+len(";base64,"):])
		if err != nil {
			return canonical.ContentBlock{}, invalid(path+".image_url.url", "data URL payload is not valid base64")
		}
		image, err := canonical.NewInlineImage(payload, mime, detail, 0, 0)
		if err != nil {
			return canonical.ContentBlock{}, translateCanonicalError(err)
		}
		return canonical.NewImageBlock(image)
	}
	if !strings.HasPrefix(url, "https://") {
		return canonical.ContentBlock{}, invalid(path+".image_url.url", "remote image URLs must use https")
	}
	image, err := canonical.NewRemoteImage(url, "", detail, 0, 0)
	if err != nil {
		return canonical.ContentBlock{}, translateCanonicalError(err)
	}
	return canonical.NewImageBlock(image)
}

func (d *requestDecoder) decodeTools(tools []wireTool) ([]canonical.ToolDefinition, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	definitions := make([]canonical.ToolDefinition, 0, len(tools))
	for index := range tools {
		tool := &tools[index]
		path := fmt.Sprintf("request.tools[%d]", index)
		if tool.Type != "" && tool.Type != "function" {
			return nil, unsupported(path + ".type")
		}
		schema, err := canonicaljson.Parse(tool.Function.Parameters, canonicaljson.DefaultLimits())
		if err != nil {
			return nil, invalid(path+".function.parameters", "parameters must be valid JSON")
		}
		definition, err := canonical.NewToolDefinition(tool.Function.Name, tool.Function.Description, schema)
		if err != nil {
			return nil, translateCanonicalError(err)
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func (d *requestDecoder) decodeToolChoice(raw json.RawMessage) (canonical.ToolChoice, error) {
	if len(raw) == 0 {
		return canonical.NewToolChoice(canonical.ToolChoiceKind("auto"), "")
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		switch asString {
		case "auto", "none", "required":
			return canonical.NewToolChoice(canonical.ToolChoiceKind(asString), "")
		default:
			return canonical.ToolChoice{}, invalid("request.tool_choice", "tool_choice string is unsupported")
		}
	}
	var choice wireToolChoice
	if err := json.Unmarshal(raw, &choice); err != nil {
		return canonical.ToolChoice{}, invalid("request.tool_choice", "tool_choice must be a string or an object")
	}
	switch choice.Type {
	case "function":
		if choice.Function.Name == "" {
			return canonical.ToolChoice{}, invalid("request.tool_choice.function.name", "specific tool_choice requires a function name")
		}
		toolChoice, err := canonical.NewToolChoice(canonical.ToolChoiceKind("specific"), choice.Function.Name)
		if err != nil {
			return canonical.ToolChoice{}, invalid("request.tool_choice.function.name", "specific tool_choice name is invalid")
		}
		return toolChoice, nil
	case "auto", "none", "required":
		return canonical.NewToolChoice(canonical.ToolChoiceKind(choice.Type), "")
	default:
		return canonical.ToolChoice{}, invalid("request.tool_choice.type", "tool_choice type is unsupported")
	}
}

func (d *requestDecoder) decodeOutput(wire wireRequest, toolCount int) (canonical.OutputConstraints, error) {
	params := canonical.OutputParams{Format: canonical.OutputFormat("text")}
	if wire.ResponseFormat != nil {
		switch wire.ResponseFormat.Type {
		case "json_object":
			params.Format = canonical.OutputFormat("json_object")
		case "json_schema":
			params.Format = canonical.OutputFormat("json_schema")
			params.SchemaName = wire.ResponseFormat.JSONSchema.Name
			schema, err := canonicaljson.Parse(wire.ResponseFormat.JSONSchema.Schema, canonicaljson.DefaultLimits())
			if err != nil {
				return canonical.OutputConstraints{}, invalid("request.response_format.json_schema.schema", "schema must be valid JSON")
			}
			params.Schema = schema
			if wire.ResponseFormat.JSONSchema.Strict {
				params.Enforcement = canonical.SchemaEnforcementStrict
			} else {
				params.Enforcement = canonical.SchemaEnforcementBestEffort
			}
		case "text":
		default:
			return canonical.OutputConstraints{}, invalid("request.response_format.type", "response_format type is unsupported")
		}
	}
	if wire.MaxTokens != nil {
		if *wire.MaxTokens <= 0 {
			return canonical.OutputConstraints{}, invalid("request.max_tokens", "max_tokens must be positive")
		}
		params.MaxTokens = *wire.MaxTokens
	}
	// Canonical requires declared tools for parallel tool calls; chat clients
	// default the flag to true unconditionally, so the canonical default is
	// false unless tools are present and the flag is explicitly enabled.
	params.ParallelToolCalls = toolCount > 0 && wire.ParallelToolCalls != nil && *wire.ParallelToolCalls
	constraints, err := canonical.NewOutputConstraints(params)
	if err != nil {
		return canonical.OutputConstraints{}, translateCanonicalError(err)
	}
	return constraints, nil
}

func translateCanonicalError(err error) error {
	var canonicalErr *canonical.Error
	if errors.As(err, &canonicalErr) {
		path := "request"
		if canonicalErr.Path != "" {
			if strings.HasPrefix(canonicalErr.Path, "request.") {
				path = canonicalErr.Path
			} else {
				path = "request." + canonicalErr.Path
			}
		}
		return &Error{Code: "invalid", Path: path, Message: "request violates canonical limits"}
	}
	return &Error{Code: "invalid", Path: "request", Message: "request is invalid"}
}
