// Package mock provides deterministic canonical event scenarios for development and tests.
package mock

import (
	"context"
	"crypto/rand"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

const defaultSlowDelay = 100 * time.Millisecond

// IDGenerator supplies canonical IDs for deterministic tests or production randomness.
type IDGenerator interface {
	ResponseID() (canonical.ResponseID, error)
	EventID() (canonical.EventID, error)
	ItemID() (canonical.ItemID, error)
	ToolCallID() (canonical.ToolCallID, error)
}

// Options configures a mock Provider.
type Options struct {
	IDs       IDGenerator
	SlowDelay time.Duration
}

// Provider implements execution using deterministic scripted scenarios.
type Provider struct {
	ids       IDGenerator
	slowDelay time.Duration
	models    []execution.Model
	idMu      sync.Mutex
}

// New constructs a deterministic mock provider.
func New(options Options) (*Provider, error) {
	if nilIDs(options.IDs) {
		return nil, execution.NewError(execution.CodeInternal, "mock.ids", 0, errors.New("ID generator is unavailable"))
	}
	if options.SlowDelay < 0 {
		return nil, execution.NewError(execution.CodeInternal, "mock.slow_delay", 0, errors.New("slow delay is invalid"))
	}
	if options.SlowDelay == 0 {
		options.SlowDelay = defaultSlowDelay
	}
	models, err := modelCatalog()
	if err != nil {
		return nil, err
	}
	return &Provider{ids: options.IDs, slowDelay: options.SlowDelay, models: models}, nil
}

// NewRandom constructs a provider backed by crypto/rand IDs.
func NewRandom(slowDelay time.Duration) (*Provider, error) {
	return New(Options{IDs: randomIDs{}, SlowDelay: slowDelay})
}

// Models returns a defensive copy of the exact sorted mock catalog.
func (p *Provider) Models(ctx context.Context) ([]execution.Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]execution.Model(nil), p.models...), nil
}

// Execute creates a single-use canonical event source for the selected mock model.
func (p *Provider) Execute(ctx context.Context, request canonical.Request) (streaming.EventSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !request.Valid() {
		return nil, execution.NewError(execution.CodeInvalidModel, "request", 0, errors.New("request is invalid"))
	}
	model, ok := p.model(request.Model())
	if !ok || !supports(model.Capabilities(), request.Capabilities()) {
		return nil, execution.NewError(execution.CodeInvalidModel, "request.model", 0, errors.New("model is unavailable"))
	}
	if request.Model() == "mock/vision" && requestImageCount(request) == 0 {
		return nil, execution.NewError(execution.CodeInvalidModel, "request.input", 0, errors.New("vision model requires an image input"))
	}
	p.idMu.Lock()
	events, finalErr, err := buildScript(p.ids, request)
	p.idMu.Unlock()
	if err != nil {
		return nil, execution.NewError(execution.CodeInternal, "mock.script", 0, errors.New("mock script generation failed"))
	}
	delay := time.Duration(0)
	if request.Model() == "mock/slow" {
		delay = p.slowDelay
	}
	return newSource(events, finalErr, delay), nil
}

func (p *Provider) model(id string) (execution.Model, bool) {
	for _, model := range p.models {
		if model.ID() == id {
			return model, true
		}
	}
	return execution.Model{}, false
}

func modelCatalog() ([]execution.Model, error) {
	text, err := canonical.NewCapabilitySet(canonical.CapabilityText)
	if err != nil {
		return nil, err
	}
	structured, err := canonical.NewCapabilitySet(canonical.CapabilityText, canonical.CapabilityStructuredOutput)
	if err != nil {
		return nil, err
	}
	reasoning, err := canonical.NewCapabilitySet(canonical.CapabilityText, canonical.CapabilityReasoningSummary)
	if err != nil {
		return nil, err
	}
	tools, err := canonical.NewCapabilitySet(canonical.CapabilityText, canonical.CapabilityTools)
	if err != nil {
		return nil, err
	}
	vision, err := canonical.NewCapabilitySet(canonical.CapabilityText, canonical.CapabilityImageInput)
	if err != nil {
		return nil, err
	}
	specs := []struct {
		id   string
		caps canonical.CapabilitySet
	}{
		{"mock/disconnect", text}, {"mock/json", structured}, {"mock/reasoning", reasoning},
		{"mock/slow", text}, {"mock/text", text}, {"mock/tool", tools}, {"mock/vision", vision},
	}
	models := make([]execution.Model, 0, len(specs))
	for _, spec := range specs {
		model, err := execution.NewModel(execution.ModelParams{ID: spec.id, Provider: "mock", Capabilities: spec.caps})
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	if err := execution.ValidateModels(models); err != nil {
		return nil, err
	}
	return models, nil
}

func supports(available, required canonical.CapabilitySet) bool {
	for _, capability := range required.Values() {
		if !available.Has(capability) {
			return false
		}
	}
	return true
}

func nilIDs(ids IDGenerator) bool {
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

type randomIDs struct{}

func (randomIDs) ResponseID() (canonical.ResponseID, error) {
	return canonical.GenerateResponseID(rand.Reader)
}
func (randomIDs) EventID() (canonical.EventID, error) { return canonical.GenerateEventID(rand.Reader) }
func (randomIDs) ItemID() (canonical.ItemID, error)   { return canonical.GenerateItemID(rand.Reader) }
func (randomIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(rand.Reader)
}

var _ execution.Executor = (*Provider)(nil)
var _ execution.ModelCatalog = (*Provider)(nil)
