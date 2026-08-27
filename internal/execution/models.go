package execution

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

const (
	maxModelIDBytes  = 128
	maxProviderBytes = 64
)

// ModelParams supplies immutable model metadata.
type ModelParams struct {
	ID           string
	Provider     string
	Capabilities canonical.CapabilitySet
}

// Model is immutable provider model metadata.
type Model struct {
	id           string
	provider     string
	capabilities canonical.CapabilitySet
}

// NewModel validates and copies provider model metadata.
func NewModel(params ModelParams) (Model, error) {
	if !validIdentity(params.ID, maxModelIDBytes, true) {
		return Model{}, NewError(CodeInvalidModel, "model.id", 0, nil)
	}
	if !validIdentity(params.Provider, maxProviderBytes, false) {
		return Model{}, NewError(CodeInvalidModel, "model.provider", 0, nil)
	}
	if !params.Capabilities.Valid() || len(params.Capabilities.Values()) == 0 {
		return Model{}, NewError(CodeInvalidModel, "model.capabilities", 0, nil)
	}
	capabilities, err := canonical.NewCapabilitySet(params.Capabilities.Values()...)
	if err != nil {
		return Model{}, NewError(CodeInvalidModel, "model.capabilities", 0, nil)
	}
	return Model{id: params.ID, provider: params.Provider, capabilities: capabilities}, nil
}

// ID returns the stable provider-qualified model identifier.
func (m Model) ID() string { return m.id }

// Provider returns the provider identifier.
func (m Model) Provider() string { return m.provider }

// Capabilities returns an immutable capability set value.
func (m Model) Capabilities() canonical.CapabilitySet { return m.capabilities }

// Valid reports whether the model metadata satisfies its constructor invariants.
func (m Model) Valid() bool {
	_, err := NewModel(ModelParams{ID: m.id, Provider: m.provider, Capabilities: m.capabilities})
	return err == nil
}

// ModelCatalog lists models in stable identifier order.
type ModelCatalog interface {
	Models(context.Context) ([]Model, error)
}

// ValidateModels requires valid models sorted by unique ID.
func ValidateModels(models []Model) error {
	for index, model := range models {
		if !model.Valid() {
			return NewError(CodeInvalidModel, "models", 0, nil)
		}
		if index > 0 && models[index-1].ID() >= model.ID() {
			return NewError(CodeInvalidModel, "models", 0, nil)
		}
	}
	return nil
}

func validIdentity(value string, maxBytes int, allowSlash bool) bool {
	if value == "" || len(value) > maxBytes || !utf8.ValidString(value) || !asciiAlphaNumeric(value[0]) {
		return false
	}
	if strings.Contains(value, "..") || strings.Contains(value, "//") || strings.HasSuffix(value, "/") {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if asciiAlphaNumeric(character) || character == '-' || character == '_' || character == '.' || allowSlash && character == '/' {
			continue
		}
		return false
	}
	return true
}

func asciiAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}
