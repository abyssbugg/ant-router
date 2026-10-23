package execution

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// toolRecordingAdapter records the tool kinds of the request it receives.
type toolRecordingAdapter struct {
	registryAdapter
	toolKinds []canonical.ToolDefinitionKind
	parallel  bool
}

func (a *toolRecordingAdapter) Execute(ctx context.Context, upstream string, request canonical.Request) (streaming.EventSource, error) {
	a.toolKinds = a.toolKinds[:0]
	a.parallel = request.Output().ParallelToolCalls()
	for _, tool := range request.Tools() {
		a.toolKinds = append(a.toolKinds, tool.Kind())
	}
	return a.registryAdapter.Execute(ctx, upstream, request)
}

func webSearchRequest(t *testing.T, model string, choiceKind canonical.ToolChoiceKind, withFunction bool) canonical.Request {
	t.Helper()
	base := registryRequest(t, model)
	tools := []canonical.ToolDefinition{canonical.NewHostedWebSearchTool()}
	if withFunction {
		schema, err := canonicaljson.Parse([]byte(`{"type":"object","properties":{}}`), canonicaljson.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		function, err := canonical.NewToolDefinition("lookup", "finds things", schema)
		if err != nil {
			t.Fatal(err)
		}
		tools = append(tools, function)
	}
	choice, err := canonical.NewToolChoice(choiceKind, "")
	if err != nil {
		t.Fatal(err)
	}
	requestID, _ := canonical.GenerateRequestID(bytes.NewReader(bytes.Repeat([]byte{3}, 16)))
	request, err := canonical.NewRequest(canonical.RequestParams{ID: requestID, Model: model, Input: base.Input(), Tools: tools, ToolChoice: choice, Output: base.Output(), Sampling: base.Sampling()})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func webSearchRegistry(t *testing.T, adapter ProviderAdapter, capabilities ...canonical.Capability) *Registry {
	t.Helper()
	registry, err := NewRegistry([]Registration{{ProviderID: "deepseek", Adapter: adapter, Models: []ModelBinding{{Model: registryModel(t, "deepseek/chat", "deepseek", capabilities...), Upstream: "deepseek-chat"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// A model without hosted web search has web search disabled: the hosted tool
// declaration is withheld from the provider instead of failing the turn.
func TestRegistryWithholdsHostedWebSearchFromModelsWithoutIt(t *testing.T) {
	adapter := &toolRecordingAdapter{registryAdapter: registryAdapter{availability: Available(), source: &registrySource{}}}
	registry := webSearchRegistry(t, adapter, canonical.CapabilityText, canonical.CapabilityTools)
	if _, err := registry.Execute(context.Background(), webSearchRequest(t, "deepseek/chat", canonical.ToolChoiceKind("auto"), true)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if adapter.calls != 1 || len(adapter.toolKinds) != 1 || adapter.toolKinds[0] != canonical.ToolDefinitionKindFunction {
		t.Fatalf("calls=%d tools=%v, want only the function tool", adapter.calls, adapter.toolKinds)
	}
}

func TestRegistryWithholdsSoleHostedWebSearchUnderAutoChoice(t *testing.T) {
	adapter := &toolRecordingAdapter{registryAdapter: registryAdapter{availability: Available(), source: &registrySource{}}}
	registry := webSearchRegistry(t, adapter, canonical.CapabilityText)
	if _, err := registry.Execute(context.Background(), webSearchRequest(t, "deepseek/chat", canonical.ToolChoiceKind("auto"), false)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if adapter.calls != 1 || len(adapter.toolKinds) != 0 {
		t.Fatalf("calls=%d tools=%v, want no tools", adapter.calls, adapter.toolKinds)
	}
}

// Withholding the only tool would silently change a required tool choice, so
// that request is rejected before the provider is called.
func TestRegistryRejectsRequiredChoiceThatOnlyHostedWebSearchCouldSatisfy(t *testing.T) {
	adapter := &toolRecordingAdapter{registryAdapter: registryAdapter{availability: Available(), source: &registrySource{}}}
	registry := webSearchRegistry(t, adapter, canonical.CapabilityText, canonical.CapabilityTools)
	_, err := registry.Execute(context.Background(), webSearchRequest(t, "deepseek/chat", canonical.ToolChoiceKind("required"), false))
	if CodeOf(err) != CodeInvalidRequest || adapter.calls != 0 {
		t.Fatalf("err=%v code=%q calls=%d, want invalid request before provider", err, CodeOf(err), adapter.calls)
	}
}

func TestRegistryKeepsHostedWebSearchForModelsThatSupportIt(t *testing.T) {
	adapter := &toolRecordingAdapter{registryAdapter: registryAdapter{availability: Available(), source: &registrySource{}}}
	registry := webSearchRegistry(t, adapter, canonical.CapabilityText, canonical.CapabilityTools, canonical.CapabilityHostedWebSearch)
	if _, err := registry.Execute(context.Background(), webSearchRequest(t, "deepseek/chat", canonical.ToolChoiceKind("auto"), true)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(adapter.toolKinds) != 2 || adapter.toolKinds[0] != canonical.ToolDefinitionKindWebSearch {
		t.Fatalf("tools=%v, want web search preserved", adapter.toolKinds)
	}
}

// webSearchRequestWith rebuilds a web-search request with extra input items
// and the given parallel tool-call setting.
func webSearchRequestWith(t *testing.T, base canonical.Request, extra []canonical.Item, parallel bool) canonical.Request {
	t.Helper()
	output, err := canonical.NewOutputConstraints(canonical.OutputParams{Format: base.Output().Format(), ParallelToolCalls: parallel})
	if err != nil {
		t.Fatal(err)
	}
	request, err := canonical.NewRequest(canonical.RequestParams{ID: base.ID(), Model: base.Model(), Input: append(base.Input(), extra...), Tools: base.Tools(), ToolChoice: base.ToolChoice(), Output: output, Sampling: base.Sampling()})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

// A prior hosted search call cannot be replayed to a model without hosted web
// search, so withholding the declaration cannot make the turn executable: the
// model is reported as unable to serve it, exactly as before withholding.
func TestRegistryRejectsHostedSearchHistoryAsInvalidModel(t *testing.T) {
	adapter := &toolRecordingAdapter{registryAdapter: registryAdapter{availability: Available(), source: &registrySource{}}}
	registry := webSearchRegistry(t, adapter, canonical.CapabilityText, canonical.CapabilityTools)
	action, err := canonical.NewHostedSearchActionSearch([]string{"ant router"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	itemID, _ := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{9}, 16)))
	call, err := canonical.NewHostedSearchCall(itemID, canonical.HostedSearchStatusCompleted, action)
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewHostedSearchCallItem(call)
	if err != nil {
		t.Fatal(err)
	}
	request := webSearchRequestWith(t, webSearchRequest(t, "deepseek/chat", canonical.ToolChoiceKind("auto"), true), []canonical.Item{item}, false)
	_, err = registry.Execute(context.Background(), request)
	var executionErr *Error
	if CodeOf(err) != CodeInvalidModel || !errors.As(err, &executionErr) || executionErr.Path != "request.model" || adapter.calls != 0 {
		t.Fatalf("err=%v code=%q calls=%d, want invalid model at request.model before provider", err, CodeOf(err), adapter.calls)
	}
}

// Withholding the only tool also drops a parallel tool-call request: with no
// tools left there is nothing to call in parallel, and the turn must proceed.
func TestRegistryWithholdsSoleHostedWebSearchWithParallelToolCalls(t *testing.T) {
	adapter := &toolRecordingAdapter{registryAdapter: registryAdapter{availability: Available(), source: &registrySource{}}}
	registry := webSearchRegistry(t, adapter, canonical.CapabilityText, canonical.CapabilityTools, canonical.CapabilityParallelTools)
	request := webSearchRequestWith(t, webSearchRequest(t, "deepseek/chat", canonical.ToolChoiceKind("auto"), false), nil, true)
	if _, err := registry.Execute(context.Background(), request); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if adapter.calls != 1 || len(adapter.toolKinds) != 0 || adapter.parallel {
		t.Fatalf("calls=%d tools=%v parallel=%v, want no tools and no parallel tool calls", adapter.calls, adapter.toolKinds, adapter.parallel)
	}
}

// A rejected rewrite reports the failing field and keeps the canonical cause.
func TestRegistryReportsWithheldWebSearchRejectionCause(t *testing.T) {
	adapter := &toolRecordingAdapter{registryAdapter: registryAdapter{availability: Available(), source: &registrySource{}}}
	registry := webSearchRegistry(t, adapter, canonical.CapabilityText, canonical.CapabilityTools)
	_, err := registry.Execute(context.Background(), webSearchRequest(t, "deepseek/chat", canonical.ToolChoiceKind("required"), false))
	var executionErr *Error
	var canonicalErr *canonical.Error
	if !errors.As(err, &executionErr) || !errors.As(err, &canonicalErr) || executionErr.Path != canonicalErr.Path || executionErr.Path == "" {
		t.Fatalf("err=%v, want execution error carrying the canonical cause and its path", err)
	}
}
