package execution

import (
	"context"
	"errors"
	"reflect"
	"sort"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// ProviderAdapter executes canonical requests against one provider instance.
// Implementations must be safe for concurrent Availability and Execute calls.
type ProviderAdapter interface {
	Availability(context.Context) (Availability, error)
	Execute(context.Context, string, canonical.Request) (streaming.EventSource, error)
}

// ModelBinding associates public model metadata with one upstream model ID.
type ModelBinding struct {
	Model    Model
	Upstream string
}

// Registration supplies one provider adapter and its model bindings.
type Registration struct {
	ProviderID string
	Adapter    ProviderAdapter
	Models     []ModelBinding
}

type registryRegistration struct {
	providerID string
	adapter    ProviderAdapter
	models     []ModelBinding
}

type boundModel struct {
	adapter  ProviderAdapter
	model    Model
	upstream string
}

// Registry exposes available public models and dispatches exact aliases once.
// It is immutable after construction and safe for concurrent use when its
// registered adapters satisfy ProviderAdapter's concurrency contract.
type Registry struct {
	registrations []registryRegistration
	bindings      map[string]boundModel
}

// NewRegistry validates, sorts, and defensively copies provider registrations.
// An empty registry is valid for control-only daemon operation.
func NewRegistry(registrations []Registration) (*Registry, error) {
	copied := append([]Registration(nil), registrations...)
	sort.SliceStable(copied, func(i, j int) bool { return copied[i].ProviderID < copied[j].ProviderID })
	registry := &Registry{bindings: make(map[string]boundModel)}
	for index, registration := range copied {
		if !validIdentity(registration.ProviderID, maxProviderBytes, false) || nilInterface(registration.Adapter) || len(registration.Models) == 0 {
			return nil, NewError(CodeInvalidModel, "providers", 0, nil)
		}
		if index > 0 && copied[index-1].ProviderID == registration.ProviderID {
			return nil, NewError(CodeInvalidModel, "providers", 0, nil)
		}
		models := append([]ModelBinding(nil), registration.Models...)
		sort.SliceStable(models, func(i, j int) bool { return models[i].Model.ID() < models[j].Model.ID() })
		for modelIndex, binding := range models {
			if !binding.Model.Valid() || binding.Model.Provider() != registration.ProviderID || !validIdentity(binding.Upstream, maxModelIDBytes, true) {
				return nil, NewError(CodeInvalidModel, "providers.models", 0, nil)
			}
			if modelIndex > 0 && models[modelIndex-1].Model.ID() == binding.Model.ID() {
				return nil, NewError(CodeInvalidModel, "providers.models", 0, nil)
			}
			if _, exists := registry.bindings[binding.Model.ID()]; exists {
				return nil, NewError(CodeInvalidModel, "providers.models", 0, nil)
			}
			registry.bindings[binding.Model.ID()] = boundModel{adapter: registration.Adapter, model: binding.Model, upstream: binding.Upstream}
		}
		registry.registrations = append(registry.registrations, registryRegistration{providerID: registration.ProviderID, adapter: registration.Adapter, models: models})
	}
	return registry, nil
}

// Models returns available public models in stable alias order.
func (r *Registry) Models(ctx context.Context) ([]Model, error) {
	if r == nil || ctx == nil {
		return nil, NewError(CodeInternal, "registry", 0, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(r.bindings))
	for _, registration := range r.registrations {
		availability, err := providerAvailability(ctx, registration.adapter)
		if err != nil {
			return nil, err
		}
		if !availability.Available() {
			continue
		}
		for _, binding := range registration.models {
			models = append(models, binding.Model)
		}
	}
	sort.SliceStable(models, func(i, j int) bool { return models[i].ID() < models[j].ID() })
	if err := ValidateModels(models); err != nil {
		return nil, err
	}
	return append([]Model(nil), models...), nil
}

// ProviderStatuses returns safe availability metadata in provider-ID order.
func (r *Registry) ProviderStatuses(ctx context.Context) ([]ProviderStatus, error) {
	if r == nil || ctx == nil {
		return nil, NewError(CodeInternal, "registry", 0, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	statuses := make([]ProviderStatus, 0, len(r.registrations))
	for _, registration := range r.registrations {
		availability, err := providerAvailability(ctx, registration.adapter)
		if err != nil {
			return nil, err
		}
		status, err := newProviderStatus(registration.providerID, availability)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return append([]ProviderStatus(nil), statuses...), nil
}

// Execute dispatches a public alias to exactly one provider attempt.
func (r *Registry) Execute(ctx context.Context, request canonical.Request) (streaming.EventSource, error) {
	if r == nil || ctx == nil {
		return nil, NewError(CodeInternal, "registry", 0, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !request.Valid() {
		return nil, NewError(CodeInvalidRequest, "request", 0, nil)
	}
	binding, exists := r.bindings[request.Model()]
	if !exists || !supportsCapabilities(binding.model.Capabilities(), request.Capabilities()) {
		return nil, NewError(CodeInvalidModel, "request.model", 0, nil)
	}
	source, err := binding.adapter.Execute(ctx, binding.upstream, request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || CodeOf(err).Valid() {
			return nil, err
		}
		return nil, NewError(CodeInternal, "provider.execute", 0, err)
	}
	if nilInterface(source) {
		return nil, NewError(CodeInternal, "provider.source", 0, nil)
	}
	return source, nil
}

func providerAvailability(ctx context.Context, adapter ProviderAdapter) (Availability, error) {
	availability, err := adapter.Availability(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || CodeOf(err).Valid() {
			return Availability{}, err
		}
		return Availability{}, NewError(CodeUnavailable, "provider.availability", 0, err)
	}
	if !availability.Valid() {
		return Availability{}, NewError(CodeInternal, "provider.availability", 0, nil)
	}
	return availability, nil
}

func supportsCapabilities(available, required canonical.CapabilitySet) bool {
	for _, capability := range required.Values() {
		if !available.Has(capability) {
			return false
		}
	}
	return true
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
