package codexcatalog

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/abyssbugg/ant-router/internal/config"
)

const (
	InstructionsVersion = 1
	TruncationByteLimit = 10_000
	instructions        = "You are a coding agent. Use the available function tools to inspect and modify the workspace. Work carefully, preserve user data, and report verified results concisely."
)

type Projection struct {
	Alias        string
	Capabilities []string
	Metadata     config.CodexModelConfig
}

func validCapability(value string) bool {
	switch value {
	case "text", "image_input", "tools", "namespace_tools", "hosted_web_search", "parallel_tools", "structured_output", "reasoning_summary", "continuation":
		return true
	default:
		return false
	}
}

type response struct {
	Models []model `json:"models"`
}
type messages struct {
	InstructionsTemplate string `json:"instructions_template"`
}
type truncation struct {
	Mode  string `json:"mode"`
	Limit int64  `json:"limit"`
}
type model struct {
	Slug                              string     `json:"slug"`
	DisplayName                       string     `json:"display_name"`
	Description                       *string    `json:"description"`
	DefaultReasoningLevel             any        `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels          []any      `json:"supported_reasoning_levels"`
	ShellType                         string     `json:"shell_type"`
	Visibility                        string     `json:"visibility"`
	SupportedInAPI                    bool       `json:"supported_in_api"`
	Priority                          int        `json:"priority"`
	AvailabilityNUX                   any        `json:"availability_nux"`
	Upgrade                           any        `json:"upgrade"`
	ModelMessages                     messages   `json:"model_messages"`
	IncludeSkillsUsageInstructions    bool       `json:"include_skills_usage_instructions"`
	IncludePluginUsageInstructions    bool       `json:"include_plugin_usage_instructions"`
	IncludeAppsUsageInstructions      bool       `json:"include_apps_usage_instructions"`
	SupportsReasoningSummaryParameter bool       `json:"supports_reasoning_summary_parameter"`
	DefaultReasoningSummary           string     `json:"default_reasoning_summary"`
	SupportVerbosity                  bool       `json:"support_verbosity"`
	DefaultVerbosity                  any        `json:"default_verbosity"`
	ApplyPatchToolType                any        `json:"apply_patch_tool_type"`
	WebSearchToolType                 string     `json:"web_search_tool_type"`
	TruncationPolicy                  truncation `json:"truncation_policy"`
	SupportsImageDetailOriginal       bool       `json:"supports_image_detail_original"`
	ContextWindow                     int64      `json:"context_window"`
	MaxContextWindow                  int64      `json:"max_context_window"`
	CompHash                          string     `json:"comp_hash"`
	EffectiveContextWindowPercent     int        `json:"effective_context_window_percent"`
	ExperimentalSupportedTools        []string   `json:"experimental_supported_tools"`
	InputModalities                   []string   `json:"input_modalities"`
	SupportsSearchTool                bool       `json:"supports_search_tool"`
	SupportsExperimentalContext       bool       `json:"supports_experimental_context"`
	UseResponsesLite                  bool       `json:"use_responses_lite"`
	NodeREPLAutoReviewRequired        bool       `json:"node_repl_auto_review_required"`
	NodeREPLDisabled                  bool       `json:"node_repl_disabled"`
	ToolMode                          string     `json:"tool_mode"`
	MultiAgentVersion                 any        `json:"multi_agent_version"`
	BaseInstructions                  string     `json:"base_instructions"`
}

func Generate(projections []Projection) ([]byte, error) {
	if len(projections) == 0 {
		return nil, fmt.Errorf("catalog: no models")
	}
	models := make([]model, 0, len(projections))
	seen := make(map[string]struct{}, len(projections))
	for i, projection := range projections {
		if projection.Alias == "" {
			return nil, fmt.Errorf("catalog: models[%d].alias is invalid", i)
		}
		if _, exists := seen[projection.Alias]; exists {
			return nil, fmt.Errorf("catalog: models[%d].alias is duplicate", i)
		}
		seen[projection.Alias] = struct{}{}
		metadata := projection.Metadata
		if !metadata.Enabled {
			return nil, fmt.Errorf("catalog: models[%d].codex is disabled", i)
		}
		for _, capability := range projection.Capabilities {
			if !validCapability(capability) {
				return nil, fmt.Errorf("catalog: models[%d].capabilities is invalid", i)
			}
		}
		if metadata.DisplayName == "" || metadata.Description == "" || metadata.ContextWindow <= 0 || metadata.ContextWindow > config.MaxCodexContextWindow || metadata.EffectiveContextWindowPercent < 1 || metadata.EffectiveContextWindowPercent > 100 || metadata.Priority < config.MinCodexPriority || metadata.Priority > config.MaxCodexPriority || (metadata.Visibility != config.CodexVisibilityList && metadata.Visibility != config.CodexVisibilityHide) {
			return nil, fmt.Errorf("catalog: models[%d].metadata is invalid", i)
		}
		modalities := []string{"text"}
		reasoning := false
		for _, capability := range projection.Capabilities {
			switch capability {
			case "image_input":
				modalities = append(modalities, "image")
			case "reasoning_summary":
				reasoning = true
			}
		}
		description := metadata.Description
		summary := "none"
		if reasoning {
			summary = "auto"
		}
		models = append(models, model{Slug: projection.Alias, DisplayName: metadata.DisplayName, Description: &description, SupportedReasoningLevels: []any{}, ShellType: "unified_exec", Visibility: string(metadata.Visibility), SupportedInAPI: true, Priority: metadata.Priority, ModelMessages: messages{InstructionsTemplate: instructions}, IncludeSkillsUsageInstructions: true, SupportsReasoningSummaryParameter: reasoning, DefaultReasoningSummary: summary, WebSearchToolType: "text", TruncationPolicy: truncation{Mode: "bytes", Limit: TruncationByteLimit}, ContextWindow: metadata.ContextWindow, MaxContextWindow: metadata.ContextWindow, CompHash: "ant-router-codex-v1", EffectiveContextWindowPercent: metadata.EffectiveContextWindowPercent, ExperimentalSupportedTools: []string{}, InputModalities: modalities, NodeREPLDisabled: true, ToolMode: "direct", BaseInstructions: instructions})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Priority != models[j].Priority {
			return models[i].Priority > models[j].Priority
		}
		return models[i].Slug < models[j].Slug
	})
	encoded, err := json.MarshalIndent(response{Models: models}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("catalog: encode: %w", err)
	}
	return append(encoded, '\n'), nil
}
