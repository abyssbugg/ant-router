package config

import (
	"strings"
	"unicode/utf8"
)

const (
	MaxCodexDisplayNameBytes       = 128
	MaxCodexDescriptionBytes       = 512
	MaxCodexContextWindow    int64 = 10_000_000
	MinCodexPriority               = -1000
	MaxCodexPriority               = 1000
)

type CodexVisibility string

const (
	CodexVisibilityList CodexVisibility = "list"
	CodexVisibilityHide CodexVisibility = "hide"
)

// CodexModelConfig contains explicit Codex model-catalog metadata.
// A nil value means the model is not published to Codex.
type CodexModelConfig struct {
	Enabled                       bool            `toml:"enabled"`
	DisplayName                   string          `toml:"display_name,omitempty"`
	Description                   string          `toml:"description,omitempty"`
	ContextWindow                 int64           `toml:"context_window,omitempty"`
	EffectiveContextWindowPercent int             `toml:"effective_context_window_percent,omitempty"`
	Priority                      int             `toml:"priority,omitempty"`
	Visibility                    CodexVisibility `toml:"visibility,omitempty"`
}

func validCodexCatalogText(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func validateCodexModel(value *CodexModelConfig, path string) error {
	if value == nil || !value.Enabled {
		return nil
	}
	if !validCodexCatalogText(value.DisplayName, MaxCodexDisplayNameBytes) {
		return providerValidationError(path+".display_name", "invalid Codex display name")
	}
	if !validCodexCatalogText(value.Description, MaxCodexDescriptionBytes) {
		return providerValidationError(path+".description", "invalid Codex description")
	}
	if value.ContextWindow <= 0 || value.ContextWindow > MaxCodexContextWindow {
		return providerValidationError(path+".context_window", "invalid Codex context window")
	}
	if value.EffectiveContextWindowPercent < 1 || value.EffectiveContextWindowPercent > 100 {
		return providerValidationError(path+".effective_context_window_percent", "invalid Codex effective context percentage")
	}
	if value.Priority < MinCodexPriority || value.Priority > MaxCodexPriority {
		return providerValidationError(path+".priority", "invalid Codex priority")
	}
	if value.Visibility != CodexVisibilityList && value.Visibility != CodexVisibilityHide {
		return providerValidationError(path+".visibility", "invalid Codex visibility")
	}
	return nil
}
