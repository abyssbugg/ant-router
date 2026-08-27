package responses

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

// MaxRequestBodyBytes bounds a Responses request before JSON parsing.
const MaxRequestBodyBytes = (32 << 20) + (64 << 10)

// IDGenerator supplies canonical identities for one decoded request.
type IDGenerator interface {
	RequestID() (canonical.RequestID, error)
	ItemID() (canonical.ItemID, error)
	ToolCallID() (canonical.ToolCallID, error)
}

// ParsedRequest contains the canonical request and client delivery mode.
type ParsedRequest struct {
	Request canonical.Request
	Stream  bool
}

type wireRequest struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input"`
	Instructions       *string         `json:"instructions"`
	Tools              json.RawMessage `json:"tools"`
	ToolChoice         json.RawMessage `json:"tool_choice"`
	ParallelToolCalls  *bool           `json:"parallel_tool_calls"`
	Text               json.RawMessage `json:"text"`
	Reasoning          json.RawMessage `json:"reasoning"`
	Temperature        *float64        `json:"temperature"`
	TopP               *float64        `json:"top_p"`
	MaxOutputTokens    *int            `json:"max_output_tokens"`
	Stream             bool            `json:"stream"`
	PreviousResponseID json.RawMessage `json:"previous_response_id"`
	Background         json.RawMessage `json:"background"`
	Store              json.RawMessage `json:"store"`
	Include            json.RawMessage `json:"include"`
	ServiceTier        json.RawMessage `json:"service_tier"`
	Metadata           json.RawMessage `json:"metadata"`
}

var allowedRequestFields = map[string]struct{}{
	"model": {}, "input": {}, "instructions": {}, "tools": {}, "tool_choice": {},
	"parallel_tool_calls": {}, "text": {}, "reasoning": {}, "temperature": {},
	"top_p": {}, "max_output_tokens": {}, "stream": {}, "previous_response_id": {},
	"background": {}, "store": {}, "include": {}, "service_tier": {}, "metadata": {},
}

// Decode strictly decodes one Responses request and adapts it to canonical form.
func Decode(reader io.Reader, ids IDGenerator) (ParsedRequest, error) {
	if reader == nil || nilIDGenerator(ids) {
		return ParsedRequest{}, newError(CodeInternal, "request", errors.New("decoder dependency is unavailable"))
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxRequestBodyBytes+1))
	if err != nil {
		return ParsedRequest{}, newError(CodeInvalidJSON, "request", errors.New("request body could not be read"))
	}
	if len(data) > MaxRequestBodyBytes {
		return ParsedRequest{}, newError(CodeLimitExceeded, "request", errors.New("request body exceeds limit"))
	}
	value, err := canonicaljson.Parse(data, canonicaljson.Limits{MaximumBytes: MaxRequestBodyBytes, MaximumDepth: 64})
	if err != nil || !value.IsObject() {
		return ParsedRequest{}, newError(CodeInvalidJSON, "request", errors.New("request must be one JSON object"))
	}
	canonicalBytes := value.Bytes()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(canonicalBytes, &fields); err != nil {
		return ParsedRequest{}, newError(CodeInvalidJSON, "request", errors.New("request could not be decoded"))
	}
	unknown := make([]string, 0)
	for field := range fields {
		if _, ok := allowedRequestFields[field]; !ok {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return ParsedRequest{}, newError(CodeUnknownField, unknown[0], errors.New("field is unknown"))
	}
	var wire wireRequest
	if err := json.Unmarshal(canonicalBytes, &wire); err != nil {
		return ParsedRequest{}, newError(CodeInvalidField, "request", errors.New("field type is invalid"))
	}
	for path, raw := range map[string]json.RawMessage{
		"previous_response_id": wire.PreviousResponseID, "background": wire.Background,
		"store": wire.Store, "include": wire.Include, "service_tier": wire.ServiceTier,
		"metadata": wire.Metadata,
	} {
		if len(raw) != 0 {
			return ParsedRequest{}, newError(CodeUnsupportedField, path, errors.New("field is unsupported"))
		}
	}
	if wire.Model == "" || strings.TrimSpace(wire.Model) != wire.Model {
		return ParsedRequest{}, newError(CodeInvalidField, "model", errors.New("model is invalid"))
	}
	tools, err := decodeTools(wire.Tools)
	if err != nil {
		return ParsedRequest{}, err
	}
	items, err := decodeInput(wire.Input, ids, tools)
	if err != nil {
		return ParsedRequest{}, err
	}
	if wire.Instructions != nil {
		instruction, err := messageItem(ids, canonical.Role("system"), *wire.Instructions, "instructions")
		if err != nil {
			return ParsedRequest{}, err
		}
		items = append([]canonical.Item{instruction}, items...)
	}
	choice, err := decodeToolChoice(wire.ToolChoice, tools)
	if err != nil {
		return ParsedRequest{}, err
	}
	parallel := wire.ParallelToolCalls != nil && *wire.ParallelToolCalls
	if parallel && len(tools) == 0 {
		return ParsedRequest{}, newError(CodeInvalidField, "parallel_tool_calls", errors.New("parallel tool calls require tools"))
	}
	output, err := decodeOutput(wire.Text, valueOrZero(wire.MaxOutputTokens), parallel)
	if err != nil {
		return ParsedRequest{}, err
	}
	reasoningSummary, err := decodeReasoning(wire.Reasoning)
	if err != nil {
		return ParsedRequest{}, err
	}
	sampling, err := canonical.NewSampling(canonical.SamplingParams{Temperature: wire.Temperature, TopP: wire.TopP})
	if err != nil {
		return ParsedRequest{}, newError(CodeInvalidField, "sampling", errors.New("sampling is invalid"))
	}
	requestID, err := ids.RequestID()
	if err != nil {
		return ParsedRequest{}, newError(CodeInternal, "request.id", errors.New("ID generation failed"))
	}
	request, err := canonical.NewRequest(canonical.RequestParams{ID: requestID, Model: wire.Model, Input: items, Tools: tools, ToolChoice: choice, Output: output, Sampling: sampling, ReasoningSummary: reasoningSummary})
	if err != nil {
		return ParsedRequest{}, newError(CodeInvalidField, "request", errors.New("request is invalid"))
	}
	return ParsedRequest{Request: request, Stream: wire.Stream}, nil
}

func decodeStringInput(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", newError(CodeInvalidField, "input", errors.New("input is required"))
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil || text == "" {
		return "", newError(CodeInvalidField, "input", errors.New("input must be non-empty text"))
	}
	return text, nil
}

func messageItem(ids IDGenerator, role canonical.Role, value, path string) (canonical.Item, error) {
	text, err := canonical.NewTextContent(value)
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path, errors.New("text is invalid"))
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path, errors.New("text block is invalid"))
	}
	itemID, err := ids.ItemID()
	if err != nil {
		return canonical.Item{}, newError(CodeInternal, path, errors.New("ID generation failed"))
	}
	message, err := canonical.NewMessage(itemID, role, []canonical.ContentBlock{block})
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path, errors.New("message is invalid"))
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		return canonical.Item{}, newError(CodeInvalidField, path, errors.New("message is invalid"))
	}
	return item, nil
}

func nilIDGenerator(ids IDGenerator) bool {
	if ids == nil {
		return true
	}
	value := reflect.ValueOf(ids)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
