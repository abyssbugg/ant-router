package canonical

import (
	"errors"
	"strings"
	"unicode/utf8"
)

type RequestParams struct {
	ID               RequestID
	Model            string
	Input            []Item
	Tools            []ToolDefinition
	ToolChoice       ToolChoice
	Output           OutputConstraints
	Sampling         Sampling
	Continuation     *Continuation
	ReasoningSummary ReasoningSummaryMode
}

// Request is an immutable provider-neutral canonical request.
type Request struct {
	id               RequestID
	model            string
	input            []Item
	tools            []ToolDefinition
	toolChoice       ToolChoice
	output           OutputConstraints
	sampling         Sampling
	continuation     *Continuation
	reasoningSummary ReasoningSummaryMode
	capabilities     CapabilitySet
}

// NewRequest validates and defensively copies a canonical request.
func NewRequest(params RequestParams) (Request, error) {
	if !params.ID.Valid() {
		return Request{}, requestError(CodeInvalidID, "request.id")
	}
	if params.Model == "" || strings.TrimSpace(params.Model) != params.Model {
		return Request{}, requestError(CodeInvalidInvariant, "request.model")
	}
	if !utf8.ValidString(params.Model) {
		return Request{}, requestError(CodeInvalidUTF8, "request.model")
	}
	if len(params.Model) > MaxModelNameBytes {
		return Request{}, requestError(CodeLimitExceeded, "request.model")
	}
	if len(params.Input) == 0 {
		return Request{}, requestError(CodeInvalidInvariant, "request.input")
	}
	if len(params.Input) > MaxInputItems {
		return Request{}, requestError(CodeLimitExceeded, "request.input")
	}
	if len(params.Tools) > MaxTools {
		return Request{}, requestError(CodeLimitExceeded, "request.tools")
	}
	choice := params.ToolChoice
	if !choice.Valid() && choice.Kind() == "" {
		choice, _ = NewToolChoice(ToolChoiceKind("auto"), "")
	}
	if !choice.Valid() {
		return Request{}, requestError(CodeInvalidInvariant, "request.tool_choice")
	}
	output := params.Output
	if !output.Valid() && output.Format() == "" {
		output, _ = NewOutputConstraints(OutputParams{Format: OutputFormat("text")})
	}
	if !output.Valid() || !params.Sampling.Valid() {
		return Request{}, requestError(CodeInvalidInvariant, "request.options")
	}
	reasoningSummary := params.ReasoningSummary
	if reasoningSummary == "" {
		reasoningSummary = ReasoningSummaryNone
	}
	if !reasoningSummary.Valid() {
		return Request{}, requestError(CodeInvalidInvariant, "request.reasoning_summary")
	}
	// Continuations bind to a configured provider ID and upstream model, while
	// request.Model is a public alias. Exact affinity is enforced by the
	// execution registry after resolving that alias.
	if params.Continuation != nil && !params.Continuation.Valid() {
		return Request{}, requestError(CodeContinuationAffinityRequired, "request.continuation")
	}
	if params.Continuation == nil {
		for _, item := range params.Input {
			if reasoning, ok := item.ReasoningSummary(); ok && len(reasoning.Summaries()) == 0 {
				return Request{}, requestError(CodeInvalidInvariant, "request.input")
			}
		}
	}
	if err := validateRequestItemsAndTools(params.Input, params.Tools, choice, output); err != nil {
		return Request{}, err
	}
	request := Request{
		id:               params.ID,
		model:            params.Model,
		input:            append([]Item(nil), params.Input...),
		tools:            append([]ToolDefinition(nil), params.Tools...),
		toolChoice:       choice,
		output:           output,
		sampling:         params.Sampling,
		reasoningSummary: reasoningSummary,
	}
	if params.Continuation != nil {
		copyValue, _ := NewContinuation(params.Continuation.Provider(), params.Continuation.Model(), params.Continuation.Bytes())
		request.continuation = &copyValue
	}
	request.capabilities, _ = computeCapabilities(request.input, request.tools, request.output, request.continuation, request.reasoningSummary)
	return request, nil
}

func validateRequestItemsAndTools(input []Item, tools []ToolDefinition, choice ToolChoice, output OutputConstraints) error {
	toolKinds := make(map[string]ToolDefinitionKind, len(tools))
	topLevelNames := make(map[string]struct{}, len(tools))
	totalCallableTools := 0
	totalDescriptionBytes := 0
	hasHostedSearchTool := false
	for _, tool := range tools {
		totalDescriptionBytes += len(tool.Description())
		if totalDescriptionBytes > MaxTotalToolDescriptionBytes {
			return requestError(CodeLimitExceeded, "request.tools")
		}
		if !tool.Valid() {
			return requestError(CodeInvalidInvariant, "request.tools")
		}
		if _, exists := topLevelNames[tool.Name()]; exists {
			return requestError(CodeInvalidInvariant, "request.tools")
		}
		topLevelNames[tool.Name()] = struct{}{}
		if tool.Kind() == ToolDefinitionKindNamespace {
			for _, child := range tool.Tools() {
				totalDescriptionBytes += len(child.Description())
				if totalDescriptionBytes > MaxTotalToolDescriptionBytes {
					return requestError(CodeLimitExceeded, "request.tools")
				}
				totalCallableTools++
				toolKinds[toolIdentity(tool.Name(), child.Name())] = child.Kind()
			}
		} else if tool.Kind() == ToolDefinitionKindFunction || tool.Kind() == ToolDefinitionKindCustom {
			totalCallableTools++
			toolKinds[toolIdentity("", tool.Name())] = tool.Kind()
		} else if tool.Kind() == ToolDefinitionKindWebSearch {
			hasHostedSearchTool = true
		}
	}
	if totalCallableTools > MaxTotalToolFunctions {
		return requestError(CodeLimitExceeded, "request.tools")
	}
	if choice.Kind() == ToolChoiceKind("specific") {
		if _, exists := toolKinds[toolIdentity(choice.Namespace(), choice.Name())]; !exists {
			return requestError(CodeInvalidInvariant, "request.tool_choice")
		}
	}
	if choice.Kind() == ToolChoiceKind("required") && len(tools) == 0 || output.ParallelToolCalls() && len(tools) == 0 {
		return requestError(CodeInvalidInvariant, "request.tools")
	}
	itemIDs := make(map[ItemID]struct{}, len(input))
	callIDs := make(map[ToolCallID]struct{})
	for _, item := range input {
		if !item.Valid() || !item.ID().Valid() {
			return requestError(CodeInvalidInvariant, "request.input")
		}
		if _, exists := itemIDs[item.ID()]; exists {
			return requestError(CodeInvalidInvariant, "request.input")
		}
		itemIDs[item.ID()] = struct{}{}
		switch item.Type() {
		case ItemType("tool_call"):
			call, _ := item.ToolCall()
			if declaredKind, declared := toolKinds[toolIdentity(call.Namespace(), call.Name())]; !declared || declaredKind != ToolDefinitionKindFunction {
				return requestError(CodeInvalidInvariant, "request.input")
			}
			if _, duplicate := callIDs[call.CallID()]; duplicate {
				return requestError(CodeInvalidInvariant, "request.input")
			}
			callIDs[call.CallID()] = struct{}{}
		case ItemType("custom_tool_call"):
			call, _ := item.CustomToolCall()
			if declaredKind, declared := toolKinds[toolIdentity(call.Namespace(), call.Name())]; !declared || declaredKind != ToolDefinitionKindCustom {
				return requestError(CodeInvalidInvariant, "request.input")
			}
			if _, duplicate := callIDs[call.CallID()]; duplicate {
				return requestError(CodeInvalidInvariant, "request.input")
			}
			callIDs[call.CallID()] = struct{}{}
		case ItemType("hosted_search_call"):
			if !hasHostedSearchTool {
				return requestError(CodeInvalidInvariant, "request.input")
			}
		case ItemType("tool_result"):
			result, _ := item.ToolResult()
			if _, exists := callIDs[result.CallID()]; !exists {
				return requestError(CodeInvalidInvariant, "request.input")
			}
		}
	}
	return nil
}

func toolIdentity(namespace, name string) string { return namespace + "\x00" + name }

func computeCapabilities(input []Item, tools []ToolDefinition, output OutputConstraints, continuation *Continuation, reasoningSummary ReasoningSummaryMode) (CapabilitySet, error) {
	var values []Capability
	for _, item := range input {
		switch item.Type() {
		case ItemType("message"):
			message, _ := item.Message()
			for _, block := range message.Content() {
				if block.Type() == ContentType("text") {
					values = append(values, CapabilityText)
				} else if block.Type() == ContentType("image") {
					values = append(values, CapabilityImageInput)
				}
			}
		case ItemType("tool_call"):
			values = append(values, CapabilityTools)
			call, _ := item.ToolCall()
			if call.Namespace() != "" {
				values = append(values, CapabilityNamespaceTools)
			}
		case ItemType("custom_tool_call"):
			values = append(values, CapabilityTools, CapabilityCustomTools)
			call, _ := item.CustomToolCall()
			if call.Namespace() != "" {
				values = append(values, CapabilityNamespaceTools)
			}
		case ItemType("hosted_search_call"):
			values = append(values, CapabilityTools, CapabilityHostedWebSearch)
		case ItemType("tool_result"):
			values = append(values, CapabilityTools)
			result, _ := item.ToolResult()
			for _, block := range result.Content() {
				if block.Type() == ContentType("text") {
					values = append(values, CapabilityText)
				} else if block.Type() == ContentType("image") {
					values = append(values, CapabilityImageInput)
				}
			}
		case ItemType("reasoning_summary"):
			values = append(values, CapabilityReasoningSummary)
		}
	}
	if len(tools) > 0 {
		values = append(values, CapabilityTools)
		for _, tool := range tools {
			if tool.Kind() == ToolDefinitionKindCustom {
				values = append(values, CapabilityCustomTools)
			}
			if tool.Kind() == ToolDefinitionKindNamespace {
				values = append(values, CapabilityNamespaceTools)
				for _, child := range tool.Tools() {
					if child.Kind() == ToolDefinitionKindCustom {
						values = append(values, CapabilityCustomTools)
					}
				}
			}
			if tool.Kind() == ToolDefinitionKindWebSearch {
				values = append(values, CapabilityHostedWebSearch)
			}
		}
	}
	if output.ParallelToolCalls() {
		values = append(values, CapabilityParallelTools)
	}
	if output.Format() != OutputFormat("text") {
		values = append(values, CapabilityStructuredOutput)
	}
	if continuation != nil {
		values = append(values, CapabilityContinuation)
	}
	if reasoningSummary != ReasoningSummaryNone {
		values = append(values, CapabilityReasoningSummary)
	}
	return NewCapabilitySet(values...)
}

// ID returns the canonical request ID.
func (r Request) ID() RequestID { return r.id }

// Model returns the provider-neutral model name or alias.
func (r Request) Model() string { return r.model }

// Input returns a defensive ordered item copy.
func (r Request) Input() []Item { return append([]Item(nil), r.input...) }

// Tools returns a defensive tool-definition copy.
func (r Request) Tools() []ToolDefinition { return append([]ToolDefinition(nil), r.tools...) }

// ToolChoice returns the immutable tool choice.
func (r Request) ToolChoice() ToolChoice { return r.toolChoice }

// Output returns immutable output constraints.
func (r Request) Output() OutputConstraints { return r.output }

// Sampling returns immutable sampling controls.
func (r Request) Sampling() Sampling { return r.sampling }

// ReasoningSummary returns the requested reasoning-summary detail.
func (r Request) ReasoningSummary() ReasoningSummaryMode { return r.reasoningSummary }

// Capabilities returns the computed immutable capability set.
func (r Request) Capabilities() CapabilitySet { return r.capabilities }

// Continuation returns a defensive continuation copy or nil.
func (r Request) Continuation() *Continuation {
	if r.continuation == nil {
		return nil
	}
	copyValue, _ := NewContinuation(r.continuation.Provider(), r.continuation.Model(), r.continuation.Bytes())
	return &copyValue
}

// WithoutContinuation returns an otherwise equivalent portable request.
func (r Request) WithoutContinuation() Request {
	copyValue := r
	copyValue.continuation = nil
	copyValue.capabilities, _ = computeCapabilities(copyValue.input, copyValue.tools, copyValue.output, nil, copyValue.reasoningSummary)
	return copyValue
}

// WithoutHostedWebSearchTools returns the request with every hosted web-search
// tool declaration removed. The result is revalidated, so a tool choice that
// only the removed declarations could satisfy is rejected rather than
// silently changed. Hosted search history items are kept and still require the
// hosted web search capability.
func (r Request) WithoutHostedWebSearchTools() (Request, error) {
	tools := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		if tool.Kind() != ToolDefinitionKindWebSearch {
			tools = append(tools, tool)
		}
	}
	if len(tools) == len(r.tools) {
		return r, nil
	}
	return NewRequest(RequestParams{ID: r.id, Model: r.model, Input: r.input, Tools: tools, ToolChoice: r.toolChoice, Output: r.output, Sampling: r.sampling, Continuation: r.continuation, ReasoningSummary: r.reasoningSummary})
}

// Valid reports whether the request satisfies all local and cross-value invariants.
func (r Request) Valid() bool {
	_, err := NewRequest(RequestParams{ID: r.id, Model: r.model, Input: r.input, Tools: r.tools, ToolChoice: r.toolChoice, Output: r.output, Sampling: r.sampling, Continuation: r.continuation, ReasoningSummary: r.reasoningSummary})
	return err == nil
}

func requestError(code ErrorCode, path string) error {
	return &Error{Code: code, Path: path, Err: errors.New("request is invalid")}
}
