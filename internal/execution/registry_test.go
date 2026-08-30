package execution

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

type registryAdapter struct {
	availability    Availability
	availabilityErr error
	source          streaming.EventSource
	executeErr      error
	calls           int
	upstream        string
	requestModel    string
}

func (a *registryAdapter) Availability(ctx context.Context) (Availability, error) {
	if err := ctx.Err(); err != nil {
		return Availability{}, err
	}
	return a.availability, a.availabilityErr
}

func (a *registryAdapter) Execute(ctx context.Context, upstream string, request canonical.Request) (streaming.EventSource, error) {
	a.calls++
	a.upstream = upstream
	a.requestModel = request.Model()
	return a.source, a.executeErr
}

type registrySource struct{}

func (*registrySource) Recv(context.Context) (canonical.Event, error) {
	return canonical.Event{}, errors.New("unused")
}
func (*registrySource) Close() error { return nil }

func registryModel(t *testing.T, id, provider string, capabilities ...canonical.Capability) Model {
	t.Helper()
	set, err := canonical.NewCapabilitySet(capabilities...)
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewModel(ModelParams{ID: id, Provider: provider, Capabilities: set})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func registryRequest(t *testing.T, model string) canonical.Request {
	t.Helper()
	requestID, _ := canonical.GenerateRequestID(bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
	itemID, _ := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{2}, 16)))
	text, _ := canonical.NewTextContent("hello")
	block, _ := canonical.NewTextBlock(text)
	message, _ := canonical.NewMessage(itemID, canonical.Role("user"), []canonical.ContentBlock{block})
	item, _ := canonical.NewMessageItem(message)
	choice, _ := canonical.NewToolChoice(canonical.ToolChoiceKind("auto"), "")
	output, _ := canonical.NewOutputConstraints(canonical.OutputParams{Format: canonical.OutputFormat("text")})
	sampling, _ := canonical.NewSampling(canonical.SamplingParams{})
	request, err := canonical.NewRequest(canonical.RequestParams{ID: requestID, Model: model, Input: []canonical.Item{item}, ToolChoice: choice, Output: output, Sampling: sampling})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestRegistryListsOnlyAvailableModelsInAliasOrder(t *testing.T) {
	available := &registryAdapter{availability: Available()}
	missing, err := Unavailable(CodeUnavailable, "OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	unavailable := &registryAdapter{availability: missing}
	registry, err := NewRegistry([]Registration{
		{ProviderID: "zeta", Adapter: unavailable, Models: []ModelBinding{{Model: registryModel(t, "zeta/model", "zeta", canonical.CapabilityText), Upstream: "zeta-upstream"}}},
		{ProviderID: "alpha", Adapter: available, Models: []ModelBinding{{Model: registryModel(t, "alpha/model", "alpha", canonical.CapabilityText), Upstream: "alpha-upstream"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	models, err := registry.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID() != "alpha/model" {
		t.Fatalf("models = %#v", models)
	}
	models[0] = Model{}
	fresh, _ := registry.Models(context.Background())
	if fresh[0].ID() != "alpha/model" {
		t.Fatal("catalog result was not defensive")
	}
}

func TestRegistryDispatchesAliasToExactUpstreamWithoutRewritingRequest(t *testing.T) {
	sentinel := &registrySource{}
	adapter := &registryAdapter{availability: Available(), source: sentinel}
	registry, err := NewRegistry([]Registration{{ProviderID: "openai", Adapter: adapter, Models: []ModelBinding{{Model: registryModel(t, "openai/public", "openai", canonical.CapabilityText), Upstream: "gpt-upstream"}}}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := registry.Execute(context.Background(), registryRequest(t, "openai/public"))
	if err != nil {
		t.Fatal(err)
	}
	if source != sentinel {
		t.Fatal("registry did not return the adapter source")
	}
	if adapter.calls != 1 || adapter.upstream != "gpt-upstream" || adapter.requestModel != "openai/public" {
		t.Fatalf("dispatch calls=%d upstream=%q request=%q", adapter.calls, adapter.upstream, adapter.requestModel)
	}
}

func TestRegistryDoesNotRetryAdapterFailure(t *testing.T) {
	failure := errors.New("provider failed")
	adapter := &registryAdapter{availability: Available(), executeErr: failure}
	registry, _ := NewRegistry([]Registration{{ProviderID: "openai", Adapter: adapter, Models: []ModelBinding{{Model: registryModel(t, "openai/public", "openai", canonical.CapabilityText), Upstream: "gpt-upstream"}}}})
	_, err := registry.Execute(context.Background(), registryRequest(t, "openai/public"))
	if !errors.Is(err, failure) || adapter.calls != 1 {
		t.Fatalf("Execute() = %v, calls=%d", err, adapter.calls)
	}
}

func TestRegistryRejectsUnknownUnsupportedAndNilSource(t *testing.T) {
	adapter := &registryAdapter{availability: Available()}
	registry, err := NewRegistry([]Registration{{ProviderID: "openai", Adapter: adapter, Models: []ModelBinding{{Model: registryModel(t, "openai/image", "openai", canonical.CapabilityImageInput), Upstream: "gpt-upstream"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"openai/unknown", "openai/image"} {
		_, executeErr := registry.Execute(context.Background(), registryRequest(t, model))
		if CodeOf(executeErr) != CodeInvalidModel || adapter.calls != 0 {
			t.Fatalf("model %q error=%v calls=%d", model, executeErr, adapter.calls)
		}
	}
	textRegistry, _ := NewRegistry([]Registration{{ProviderID: "openai", Adapter: adapter, Models: []ModelBinding{{Model: registryModel(t, "openai/text", "openai", canonical.CapabilityText), Upstream: "gpt-upstream"}}}})
	_, executeErr := textRegistry.Execute(context.Background(), registryRequest(t, "openai/text"))
	if CodeOf(executeErr) != CodeInternal || adapter.calls != 1 {
		t.Fatalf("nil source error=%v calls=%d", executeErr, adapter.calls)
	}
}

func TestRegistryRejectsInvalidRegistrations(t *testing.T) {
	validModel := registryModel(t, "openai/model", "openai", canonical.CapabilityText)
	var nilAdapter *registryAdapter
	tests := []struct {
		name          string
		registrations []Registration
	}{
		{"typed nil adapter", []Registration{{ProviderID: "openai", Adapter: nilAdapter, Models: []ModelBinding{{Model: validModel, Upstream: "gpt"}}}}},
		{"duplicate provider", []Registration{{ProviderID: "openai", Adapter: &registryAdapter{}, Models: []ModelBinding{{Model: validModel, Upstream: "gpt"}}}, {ProviderID: "openai", Adapter: &registryAdapter{}, Models: []ModelBinding{{Model: validModel, Upstream: "gpt"}}}}},
		{"provider mismatch", []Registration{{ProviderID: "other", Adapter: &registryAdapter{}, Models: []ModelBinding{{Model: validModel, Upstream: "gpt"}}}}},
		{"missing upstream", []Registration{{ProviderID: "openai", Adapter: &registryAdapter{}, Models: []ModelBinding{{Model: validModel}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewRegistry(tt.registrations); CodeOf(err) != CodeInvalidModel {
				t.Fatalf("NewRegistry() error = %v", err)
			}
		})
	}
}

func TestRegistryPreservesCancellation(t *testing.T) {
	registry, _ := NewRegistry(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := registry.Models(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Models() = %v", err)
	}
	if _, err := registry.Execute(ctx, registryRequest(t, "openai/model")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() = %v", err)
	}
}

func TestRegistryReportsSafeProviderAvailability(t *testing.T) {
	missing, _ := Unavailable(CodeUnavailable, "OPENAI_API_KEY")
	registry, err := NewRegistry([]Registration{{ProviderID: "openai", Adapter: &registryAdapter{availability: missing}, Models: []ModelBinding{{Model: registryModel(t, "openai/public", "openai", canonical.CapabilityText), Upstream: "gpt-upstream"}}}})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := registry.ProviderStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].ProviderID() != "openai" || statuses[0].Availability().Available() || statuses[0].Availability().Reference() != "OPENAI_API_KEY" {
		t.Fatalf("statuses = %#v", statuses)
	}
}

var _ ProviderAdapter = (*registryAdapter)(nil)
var _ Executor = (*Registry)(nil)
var _ ModelCatalog = (*Registry)(nil)
