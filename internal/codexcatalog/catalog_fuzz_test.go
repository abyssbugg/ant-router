package codexcatalog

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/abyssbugg/ant-router/internal/config"
)

func FuzzGenerate(f *testing.F) {
	f.Add("openai/gpt-test", "Test model", "Test description", int64(128000), 90, 10, "text,image_input")
	f.Add("", "", "", int64(0), 0, 0, "")
	f.Fuzz(func(t *testing.T, alias, display, description string, contextWindow int64, percent, priority int, capabilities string) {
		projection := Projection{
			Alias:        alias,
			Capabilities: splitCapabilities(capabilities),
			Metadata:     config.CodexModelConfig{Enabled: true, DisplayName: display, Description: description, ContextWindow: contextWindow, EffectiveContextWindowPercent: percent, Priority: priority, Visibility: config.CodexVisibilityList},
		}
		first, err := Generate([]Projection{projection})
		if err != nil {
			return
		}
		second, err := Generate([]Projection{projection})
		if err != nil {
			t.Fatalf("second generation failed: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatal("generation is not deterministic")
		}
		if !json.Valid(first) {
			t.Fatal("generated catalog is not valid JSON")
		}
	})
}

func splitCapabilities(value string) []string {
	if value == "" {
		return nil
	}
	var values []string
	start := 0
	for index := 0; index <= len(value); index++ {
		if index == len(value) || value[index] == ',' {
			values = append(values, value[start:index])
			start = index + 1
		}
	}
	return values
}
