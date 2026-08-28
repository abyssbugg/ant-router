package execution

import (
	"context"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

type testCatalog struct{ models []Model }

func (c testCatalog) Models(context.Context) ([]Model, error) {
	return append([]Model(nil), c.models...), nil
}

func TestModelIsValidatedAndImmutable(t *testing.T) {
	capabilities, err := canonical.NewCapabilitySet(canonical.CapabilityText, canonical.CapabilityTools)
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewModel(ModelParams{ID: "mock/text-v1", Provider: "mock", Capabilities: capabilities})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	if model.ID() != "mock/text-v1" || model.Provider() != "mock" {
		t.Fatalf("model identity = %q/%q", model.Provider(), model.ID())
	}
	got := model.Capabilities().Values()
	got[0] = canonical.CapabilityImageInput
	if model.Capabilities().Has(canonical.CapabilityImageInput) {
		t.Fatal("model capabilities changed through returned slice")
	}
	var _ ModelCatalog = testCatalog{}
}

func TestNewModelRejectsUnsafeIdentity(t *testing.T) {
	capabilities, _ := canonical.NewCapabilitySet(canonical.CapabilityText)
	for _, params := range []ModelParams{
		{ID: "", Provider: "mock", Capabilities: capabilities},
		{ID: "../secret", Provider: "mock", Capabilities: capabilities},
		{ID: "mock text", Provider: "mock", Capabilities: capabilities},
		{ID: "mock/text", Provider: "", Capabilities: capabilities},
		{ID: "mock/text", Provider: "mock/provider", Capabilities: capabilities},
	} {
		if _, err := NewModel(params); err == nil {
			t.Fatalf("NewModel(%q, %q) succeeded", params.ID, params.Provider)
		}
	}
}

func TestValidateModelsRequiresSortedUniqueValidModels(t *testing.T) {
	caps, _ := canonical.NewCapabilitySet(canonical.CapabilityText)
	first, _ := NewModel(ModelParams{ID: "mock/a", Provider: "mock", Capabilities: caps})
	second, _ := NewModel(ModelParams{ID: "mock/b", Provider: "mock", Capabilities: caps})
	if err := ValidateModels([]Model{first, second}); err != nil {
		t.Fatalf("ValidateModels() error = %v", err)
	}
	for name, models := range map[string][]Model{
		"unsorted":  {second, first},
		"duplicate": {first, first},
		"zero":      {{}},
	} {
		if err := ValidateModels(models); err == nil {
			t.Fatalf("%s catalog succeeded", name)
		}
	}
}
