package execution

import (
	"bytes"
	"context"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// toolRecordingAdapter records the tool kinds of the request it receives.
type toolRecordingAdapter struct {
	registryAdapter
	toolKinds []canonical.ToolDefinitionKind
}

func (a *toolRecordingAdapter) Execute(ctx context.Context, upstream string, request canonical.Request) (streaming.EventSource, error) {
	a.toolKinds = a.toolKinds[:0]
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
