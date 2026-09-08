package responses

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	openaicontinuation "github.com/abyssbugg/ant-router/internal/openai/continuation"
)

// MaxRequestBodyBytes bounds a Responses request before JSON parsing.
const MaxRequestBodyBytes = (32 << 20) + (64 << 10)

// IDGenerator supplies canonical identities for one decoded request.
type IDGenerator interface {
	RequestID() (canonical.RequestID, error)
	ItemID() (canonical.ItemID, error)
	ToolCallID() (canonical.ToolCallID, error)
}

// AffinityResolver maps a public model alias to its configured provider and
// upstream model without resolving credentials or provider availability.
type AffinityResolver interface {
	ResolveModel(alias string) (providerID string, upstreamModel string, err error)
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
	PromptCacheKey     json.RawMessage `json:"prompt_cache_key"`
	Metadata           json.RawMessage `json:"metadata"`
	ClientMetadata     json.RawMessage `json:"client_metadata"`
}

var allowedRequestFields = map[string]struct{}{
	"model": {}, "input": {}, "instructions": {}, "tools": {}, "tool_choice": {},
	"parallel_tool_calls": {}, "text": {}, "reasoning": {}, "temperature": {},
	"top_p": {}, "max_output_tokens": {}, "stream": {}, "previous_response_id": {},
	"background": {}, "store": {}, "include": {}, "service_tier": {}, "metadata": {},
	"prompt_cache_key": {},
	"client_metadata":  {},
}

// Decode strictly decodes one Responses request without continuation affinity.
func Decode(reader io.Reader, ids IDGenerator) (ParsedRequest, error) {
	return decode(reader, ids, nil)
}

// DecodeWithAffinity strictly decodes a request and binds replayed reasoning
// items to the configured provider and upstream model.
func DecodeWithAffinity(reader io.Reader, ids IDGenerator, resolver AffinityResolver) (ParsedRequest, error) {
	if nilAffinityResolver(resolver) {
		return ParsedRequest{}, newError(CodeInternal, "request.continuation", errors.New("affinity resolver is unavailable"))
	}
	return decode(reader, ids, resolver)
}

func decode(reader io.Reader, ids IDGenerator, resolver AffinityResolver) (ParsedRequest, error) {
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
	unsupported := []struct {
		path string
		raw  json.RawMessage
	}{
		{path: "background", raw: wire.Background},
		{path: "metadata", raw: wire.Metadata},
		{path: "previous_response_id", raw: wire.PreviousResponseID},
	}
	for _, field := range unsupported {
		if len(field.raw) != 0 {
			return ParsedRequest{}, newError(CodeUnsupportedField, field.path, errors.New("field is unsupported"))
		}
	}
	if err := validateOperationalString(wire.PromptCacheKey, "prompt_cache_key"); err != nil {
		return ParsedRequest{}, err
	}
	if err := validateOperationalString(wire.ServiceTier, "service_tier"); err != nil {
		return ParsedRequest{}, err
	}
	if err := validateClientMetadata(wire.ClientMetadata); err != nil {
		return ParsedRequest{}, err
	}
	if err := decodeStatelessControls(wire.Store, wire.Include); err != nil {
		return ParsedRequest{}, err
	}
	if wire.Model == "" || strings.TrimSpace(wire.Model) != wire.Model {
		return ParsedRequest{}, newError(CodeInvalidField, "model", errors.New("model is invalid"))
	}
	tools, err := decodeTools(wire.Tools)
	if err != nil {
		return ParsedRequest{}, err
	}
	decoded, err := decodeInput(wire.Input, ids, tools)
	if err != nil {
		return ParsedRequest{}, err
	}
	items := decoded.items
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
	var continuation *canonical.Continuation
	if len(decoded.continuationEntries) > 0 {
		if nilAffinityResolver(resolver) {
			return ParsedRequest{}, newError(CodeInternal, "request.continuation", errors.New("affinity resolver is unavailable"))
		}
		providerID, upstreamModel, err := resolver.ResolveModel(wire.Model)
		if err != nil || providerID == "" || upstreamModel == "" {
			return ParsedRequest{}, newError(CodeInvalidField, "model", errors.New("model affinity is invalid"))
		}
		value, err := openaicontinuation.EncodeContinuation(providerID, upstreamModel, decoded.continuationEntries)
		if err != nil {
			return ParsedRequest{}, newError(CodeInvalidField, "input", errors.New("continuation is invalid"))
		}
		continuation = &value
	}
	request, err := canonical.NewRequest(canonical.RequestParams{ID: requestID, Model: wire.Model, Input: items, Tools: tools, ToolChoice: choice, Output: output, Sampling: sampling, ReasoningSummary: reasoningSummary, Continuation: continuation})
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

func decodeStatelessControls(store, include json.RawMessage) error {
	if len(store) > 0 {
		var value *bool
		if json.Unmarshal(store, &value) != nil || value == nil {
			return newError(CodeInvalidField, "store", errors.New("store is invalid"))
		}
		if *value {
			return newError(CodeUnsupportedField, "store", errors.New("stored responses are unsupported"))
		}
	}
	if len(include) == 0 {
		return nil
	}
	var values []string
	if json.Unmarshal(include, &values) != nil || len(values) == 0 {
		return newError(CodeInvalidField, "include", errors.New("include is invalid"))
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		path := "include[" + strconv.Itoa(index) + "]"
		if value != "reasoning.encrypted_content" {
			return newError(CodeUnsupportedField, path, errors.New("include value is unsupported"))
		}
		if _, exists := seen[value]; exists {
			return newError(CodeInvalidField, path, errors.New("include value is duplicated"))
		}
		seen[value] = struct{}{}
	}
	return nil
}

const maxOperationalHintBytes = 1024

func validateOperationalString(raw json.RawMessage, path string) error {
	if len(raw) == 0 {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" || len(value) > maxOperationalHintBytes || containsControl(value) {
		return newError(CodeInvalidField, path, errors.New("operational hint is invalid"))
	}
	return nil
}

const (
	maxClientMetadataBytes      = 64 << 10
	maxClientMetadataEntries    = 32
	maxClientMetadataKeyBytes   = 256
	maxClientMetadataValueBytes = 8 << 10
)

func validateClientMetadata(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > maxClientMetadataBytes {
		return newError(CodeLimitExceeded, "client_metadata", errors.New("client metadata exceeds limit"))
	}
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata == nil {
		return newError(CodeInvalidField, "client_metadata", errors.New("client metadata is invalid"))
	}
	if len(metadata) > maxClientMetadataEntries {
		return newError(CodeLimitExceeded, "client_metadata", errors.New("client metadata exceeds limit"))
	}
	for key, value := range metadata {
		if key == "" || len(key) > maxClientMetadataKeyBytes || containsControl(key) {
			return newError(CodeInvalidField, "client_metadata", errors.New("client metadata is invalid"))
		}
		if len(value) > maxClientMetadataValueBytes || containsControl(value) {
			return newError(CodeInvalidField, "client_metadata", errors.New("client metadata is invalid"))
		}
	}
	return nil
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func nilAffinityResolver(resolver AffinityResolver) bool {
	if resolver == nil {
		return true
	}
	value := reflect.ValueOf(resolver)
	return value.Kind() == reflect.Pointer && value.IsNil()
}
