package daemon

import (
	"context"
	"reflect"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/execution"
	anthropicprovider "github.com/abyssbugg/ant-router/internal/provider/anthropic"
	mockprovider "github.com/abyssbugg/ant-router/internal/provider/mock"
	openaiprovider "github.com/abyssbugg/ant-router/internal/provider/openai"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

type providerRuntime struct {
	registry *execution.Registry
	kinds    map[string]config.ProviderKind
}

func buildProviderRuntime(configuration config.Config, source credential.Source) (*providerRuntime, error) {
	if nilInterface(source) {
		return nil, execution.NewError(execution.CodeInternal, "providers.credential_source", 0, nil)
	}
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	registrations := make([]execution.Registration, 0, len(configuration.Providers))
	kinds := make(map[string]config.ProviderKind)
	for _, providerConfig := range configuration.Providers {
		if !providerConfig.Enabled {
			continue
		}
		registration, err := buildProviderRegistration(providerConfig, source)
		if err != nil {
			return nil, err
		}
		registrations = append(registrations, registration)
		kinds[providerConfig.ID] = providerConfig.Kind
	}
	registry, err := execution.NewRegistry(registrations)
	if err != nil {
		return nil, err
	}
	return &providerRuntime{registry: registry, kinds: kinds}, nil
}

func buildProviderRegistration(providerConfig config.ProviderConfig, source credential.Source) (execution.Registration, error) {
	switch providerConfig.Kind {
	case config.ProviderKindMock:
		provider, err := mockprovider.NewRandom(0)
		if err != nil {
			return execution.Registration{}, err
		}
		models, err := provider.Models(context.Background())
		if err != nil {
			return execution.Registration{}, err
		}
		bindings := make([]execution.ModelBinding, len(models))
		for index, model := range models {
			bindings[index] = execution.ModelBinding{Model: model, Upstream: model.ID()}
		}
		return execution.Registration{ProviderID: providerConfig.ID, Adapter: mockAdapter{provider: provider}, Models: bindings}, nil
	case config.ProviderKindOpenAI:
		ref, err := credential.NewRef(credential.SourceEnv, providerConfig.Credential.Name)
		if err != nil {
			return execution.Registration{}, err
		}
		provider, err := openaiprovider.NewProviderForID(providerConfig.ID, source, ref)
		if err != nil {
			return execution.Registration{}, err
		}
		bindings := make([]execution.ModelBinding, len(providerConfig.Models))
		for index, modelConfig := range providerConfig.Models {
			capabilities := make([]canonical.Capability, len(modelConfig.Capabilities))
			for capabilityIndex, value := range modelConfig.Capabilities {
				capabilities[capabilityIndex] = canonical.Capability(value)
			}
			set, err := canonical.NewCapabilitySet(capabilities...)
			if err != nil {
				return execution.Registration{}, err
			}
			model, err := execution.NewModel(execution.ModelParams{ID: modelConfig.Alias, Provider: providerConfig.ID, Capabilities: set})
			if err != nil {
				return execution.Registration{}, err
			}
			bindings[index] = execution.ModelBinding{Model: model, Upstream: modelConfig.Upstream}
		}
		return execution.Registration{ProviderID: providerConfig.ID, Adapter: provider, Models: bindings}, nil
	case config.ProviderKindAnthropic:
		ref, err := credential.NewRef(credential.SourceEnv, providerConfig.Credential.Name)
		if err != nil {
			return execution.Registration{}, err
		}
		limits := make([]anthropicprovider.ModelLimits, len(providerConfig.Models))
		bindings := make([]execution.ModelBinding, len(providerConfig.Models))
		for index, modelConfig := range providerConfig.Models {
			capabilities := make([]canonical.Capability, len(modelConfig.Capabilities))
			for capabilityIndex, value := range modelConfig.Capabilities {
				capabilities[capabilityIndex] = canonical.Capability(value)
			}
			set, err := canonical.NewCapabilitySet(capabilities...)
			if err != nil {
				return execution.Registration{}, err
			}
			model, err := execution.NewModel(execution.ModelParams{ID: modelConfig.Alias, Provider: providerConfig.ID, Capabilities: set})
			if err != nil {
				return execution.Registration{}, err
			}
			limits[index] = anthropicprovider.ModelLimits{Upstream: modelConfig.Upstream, DefaultMaxOutputTokens: modelConfig.DefaultMaxOutputTokens, MaxOutputTokens: modelConfig.MaxOutputTokens}
			bindings[index] = execution.ModelBinding{Model: model, Upstream: modelConfig.Upstream}
		}
		provider, err := anthropicprovider.NewProvider(source, ref, limits)
		if err != nil {
			return execution.Registration{}, err
		}
		return execution.Registration{ProviderID: providerConfig.ID, Adapter: provider, Models: bindings}, nil
	default:
		return execution.Registration{}, execution.NewError(execution.CodeInternal, "providers.kind", 0, nil)
	}
}

func (r *providerRuntime) summaries(ctx context.Context) ([]control.ProviderSummary, error) {
	if r == nil || r.registry == nil || ctx == nil {
		return nil, execution.NewError(execution.CodeInternal, "providers.status", 0, nil)
	}
	statuses, err := r.registry.ProviderStatuses(ctx)
	if err != nil {
		return nil, err
	}
	summaries := make([]control.ProviderSummary, len(statuses))
	for index, status := range statuses {
		availability := status.Availability()
		summaries[index] = control.ProviderSummary{
			ID:        status.ProviderID(),
			Kind:      string(r.kinds[status.ProviderID()]),
			Available: availability.Available(),
			Code:      string(availability.Code()),
			Reference: availability.Reference(),
		}
	}
	return summaries, nil
}

type mockAdapter struct{ provider *mockprovider.Provider }

func (a mockAdapter) Availability(context.Context) (execution.Availability, error) {
	if a.provider == nil {
		return execution.Availability{}, execution.NewError(execution.CodeInternal, "provider.mock", 0, nil)
	}
	return execution.Available(), nil
}

func (a mockAdapter) Execute(ctx context.Context, upstream string, request canonical.Request) (streaming.EventSource, error) {
	if a.provider == nil || upstream != request.Model() {
		return nil, execution.NewError(execution.CodeInvalidModel, "request.model", 0, nil)
	}
	return a.provider.Execute(ctx, request)
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

var _ execution.ProviderAdapter = mockAdapter{}
