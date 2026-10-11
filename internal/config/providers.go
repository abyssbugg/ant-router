package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

const (
	MaxProviders         = 16
	MaxModelsPerProvider = 128
	MaxProviderIDBytes   = 64
	MaxModelIDBytes      = 128
	MaxCredentialName    = 128
)

// ProviderKind identifies a configured provider implementation.
type ProviderKind string

const (
	ProviderKindMock      ProviderKind = "mock"
	ProviderKindOpenAI    ProviderKind = "openai"
	ProviderKindAnthropic ProviderKind = "anthropic"
)

// CredentialSourceKind identifies a credential lookup mechanism.
type CredentialSourceKind string

const CredentialSourceEnv CredentialSourceKind = "env"

// CredentialRef names a credential without storing its secret value.
type CredentialRef struct {
	Source CredentialSourceKind `toml:"source"`
	Name   string               `toml:"name"`
}

// ProviderModelConfig maps a public model alias to a provider model.
type ProviderModelConfig struct {
	Alias                  string            `toml:"alias"`
	Upstream               string            `toml:"upstream"`
	Capabilities           []string          `toml:"capabilities"`
	DefaultMaxOutputTokens int64             `toml:"default_max_output_tokens,omitempty"`
	MaxOutputTokens        int64             `toml:"max_output_tokens,omitempty"`
	Codex                  *CodexModelConfig `toml:"codex,omitempty"`
}

// ProviderConfig defines one provider instance and its public models.
type ProviderConfig struct {
	ID         string                `toml:"id"`
	Kind       ProviderKind          `toml:"kind"`
	Enabled    bool                  `toml:"enabled"`
	Credential *CredentialRef        `toml:"credential,omitempty"`
	Models     []ProviderModelConfig `toml:"models,omitempty"`
}

var (
	providerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	envNamePattern    = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

var knownProviderCapabilities = map[string]struct{}{
	"continuation":      {},
	"custom_tools":      {},
	"image_input":       {},
	"namespace_tools":   {},
	"hosted_web_search": {},
	"parallel_tools":    {},
	"reasoning_summary": {},
	"structured_output": {},
	"text":              {},
	"tools":             {},
}

// codexBaselineCapabilities is the complete written baseline required by
// Codex-enabled OpenAI models under schema version 6.
var codexBaselineCapabilities = []string{"custom_tools", "hosted_web_search", "namespace_tools", "parallel_tools", "text", "tools"}

// v5KnownProviderCapabilities is the capability set known to schema
// version 5. It excludes custom_tools, which was introduced in v6.
var v5KnownProviderCapabilities = map[string]struct{}{
	"continuation":      {},
	"image_input":       {},
	"namespace_tools":   {},
	"hosted_web_search": {},
	"parallel_tools":    {},
	"reasoning_summary": {},
	"structured_output": {},
	"text":              {},
	"tools":             {},
}

// v5LegalCapabilities reports whether a capability list could have been
// written under schema version 5 rules: non-empty, all entries known,
// no duplicates, and hosted_web_search, namespace_tools, and
// parallel_tools each requiring the tools capability as-written.
// Migration may only complete capability sets that were legal v5
// documents; illegal sets are left untouched so validation rejects
// them with the original v5-era errors.
func v5LegalCapabilities(capabilities []string) bool {
	if len(capabilities) == 0 {
		return false
	}
	hasTools := false
	requiresTools := false
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if _, known := v5KnownProviderCapabilities[capability]; !known {
			return false
		}
		if _, duplicate := seen[capability]; duplicate {
			return false
		}
		seen[capability] = struct{}{}
		switch capability {
		case "tools":
			hasTools = true
		case "hosted_web_search", "namespace_tools", "parallel_tools":
			requiresTools = true
		}
	}
	return !requiresTools || hasTools
}

// completeV5CodexBaselines migrates schema version 5 providers to the
// schema version 6 Codex capability baseline. Version 5 predates the
// written baseline, so its legal Codex-enabled models are completed
// rather than rejected. Models whose capability sets were invalid as
// v5 documents are left untouched and rejected by validation with the
// v5-era errors. Normalization sorts the resulting capabilities.
func completeV5CodexBaselines(c Config) Config {
	completed := c.clone()
	for i := range completed.Providers {
		if completed.Providers[i].Kind != ProviderKindOpenAI {
			continue
		}
		for j := range completed.Providers[i].Models {
			model := &completed.Providers[i].Models[j]
			if model.Codex == nil || !model.Codex.Enabled {
				continue
			}
			missing := missingCodexBaseline(model.Capabilities)
			if len(missing) == 0 {
				continue
			}
			if !v5LegalCapabilities(model.Capabilities) {
				continue
			}
			model.Capabilities = append(model.Capabilities, missing...)
		}
	}
	return completed
}

func missingCodexBaseline(capabilities []string) []string {
	present := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		present[capability] = struct{}{}
	}
	missing := make([]string, 0, len(codexBaselineCapabilities))
	for _, capability := range codexBaselineCapabilities {
		if _, ok := present[capability]; !ok {
			missing = append(missing, capability)
		}
	}
	return missing
}

func (c Config) clone() Config {
	cloned := c
	if c.Providers != nil {
		cloned.Providers = make([]ProviderConfig, len(c.Providers))
	}
	for i := range c.Providers {
		cloned.Providers[i] = c.Providers[i]
		if c.Providers[i].Credential != nil {
			credential := *c.Providers[i].Credential
			cloned.Providers[i].Credential = &credential
		}
		if c.Providers[i].Models != nil {
			cloned.Providers[i].Models = make([]ProviderModelConfig, len(c.Providers[i].Models))
		}
		for j := range c.Providers[i].Models {
			cloned.Providers[i].Models[j] = c.Providers[i].Models[j]
			cloned.Providers[i].Models[j].Capabilities = append([]string(nil), c.Providers[i].Models[j].Capabilities...)
			if c.Providers[i].Models[j].Codex != nil {
				codex := *c.Providers[i].Models[j].Codex
				cloned.Providers[i].Models[j].Codex = &codex
			}
		}
	}
	return cloned
}

func normalizeProviders(c Config) Config {
	c = c.clone()
	if len(c.Providers) == 0 && c.MockProvider.Enabled {
		c.Providers = []ProviderConfig{{ID: "mock", Kind: ProviderKindMock, Enabled: true}}
	}
	sort.SliceStable(c.Providers, func(i, j int) bool { return c.Providers[i].ID < c.Providers[j].ID })
	c.MockProvider.Enabled = false
	for i := range c.Providers {
		sort.SliceStable(c.Providers[i].Models, func(a, b int) bool {
			return c.Providers[i].Models[a].Alias < c.Providers[i].Models[b].Alias
		})
		for j := range c.Providers[i].Models {
			sort.Strings(c.Providers[i].Models[j].Capabilities)
		}
		if c.Providers[i].Kind == ProviderKindMock && c.Providers[i].Enabled {
			c.MockProvider.Enabled = true
		}
	}
	return c
}

func fixedMockModelAliases() [8]string {
	return [8]string{"mock/codex", "mock/disconnect", "mock/json", "mock/reasoning", "mock/slow", "mock/text", "mock/tool", "mock/vision"}
}

func isFixedMockModelAlias(alias string) bool {
	for _, known := range fixedMockModelAliases() {
		if alias == known {
			return true
		}
	}
	return false
}

func validateMockCodexModels(models []ProviderModelConfig, path string) error {
	seen := make(map[string]bool, len(models))
	for index, model := range models {
		prefix := fmt.Sprintf("%s[%d]", path, index)
		if !isFixedMockModelAlias(model.Alias) || seen[model.Alias] {
			return providerValidationError(prefix+".alias", "invalid mock model alias")
		}
		seen[model.Alias] = true
		if model.Upstream != "" || len(model.Capabilities) != 0 || model.DefaultMaxOutputTokens != 0 || model.MaxOutputTokens != 0 {
			return providerValidationError(prefix, "mock model only accepts Codex metadata")
		}
		if model.Codex == nil || !model.Codex.Enabled {
			return providerValidationError(prefix+".codex", "enabled Codex metadata is required")
		}
		if err := validateCodexModel(model.Codex, prefix+".codex"); err != nil {
			return err
		}
	}
	return nil
}

func validateProviders(c Config) error {
	if len(c.Providers) > MaxProviders {
		return providerValidationError("providers", "exceeds provider limit")
	}
	providerIDs := make(map[string]struct{}, len(c.Providers))
	aliases := make(map[string]struct{})
	for _, provider := range c.Providers {
		if provider.Kind == ProviderKindMock {
			for _, alias := range fixedMockModelAliases() {
				aliases[alias] = struct{}{}
			}
		}
	}
	mockSeen := false
	for i, provider := range c.Providers {
		prefix := fmt.Sprintf("providers[%d]", i)
		if !validProviderID(provider.ID) {
			return providerValidationError(prefix+".id", "invalid provider identifier")
		}
		if _, exists := providerIDs[provider.ID]; exists {
			return providerValidationError(prefix+".id", "duplicate provider identifier")
		}
		providerIDs[provider.ID] = struct{}{}
		switch provider.Kind {
		case ProviderKindMock:
			if mockSeen || provider.ID != "mock" {
				return providerValidationError(prefix+".kind", "only the fixed mock provider is supported")
			}
			mockSeen = true
			if provider.Credential != nil {
				return providerValidationError(prefix+".credential", "mock provider does not accept credentials")
			}
			if err := validateMockCodexModels(provider.Models, prefix+".models"); err != nil {
				return err
			}
		case ProviderKindOpenAI, ProviderKindAnthropic:
			if provider.Credential == nil {
				return providerValidationError(prefix+".credential", "credential reference is required")
			}
			if provider.Credential.Source != CredentialSourceEnv {
				return providerValidationError(prefix+".credential.source", "unsupported credential source")
			}
			if len(provider.Credential.Name) == 0 || len(provider.Credential.Name) > MaxCredentialName || !envNamePattern.MatchString(provider.Credential.Name) {
				return providerValidationError(prefix+".credential.name", "invalid environment variable name")
			}
			// Provider credentials reach only the provider's official
			// endpoint, so a mis-scoped name would silently ship another
			// secret's value there; require the kind-scoped prefix.
			switch provider.Kind {
			case ProviderKindOpenAI:
				if !strings.HasPrefix(provider.Credential.Name, "OPENAI_") {
					return providerValidationError(prefix+".credential.name", "openai credential must use an OPENAI_ prefixed environment variable")
				}
			case ProviderKindAnthropic:
				if !strings.HasPrefix(provider.Credential.Name, "ANTHROPIC_") {
					return providerValidationError(prefix+".credential.name", "anthropic credential must use an ANTHROPIC_ prefixed environment variable")
				}
			}
			if len(provider.Models) == 0 || len(provider.Models) > MaxModelsPerProvider {
				return providerValidationError(prefix+".models", "invalid model count")
			}
			for j, model := range provider.Models {
				modelPrefix := fmt.Sprintf("%s.models[%d]", prefix, j)
				if !validModelIdentifier(model.Alias) {
					return providerValidationError(modelPrefix+".alias", "invalid public model alias")
				}
				if _, exists := aliases[model.Alias]; exists {
					return providerValidationError(modelPrefix+".alias", "duplicate public model alias")
				}
				aliases[model.Alias] = struct{}{}
				if !validModelIdentifier(model.Upstream) {
					return providerValidationError(modelPrefix+".upstream", "invalid upstream model identifier")
				}
				if len(model.Capabilities) == 0 {
					return providerValidationError(modelPrefix+".capabilities", "at least one capability is required")
				}
				hasText := false
				hasTools := false
				customIndex := -1
				namespaceIndex := -1
				hostedSearchIndex := -1
				parallelIndex := -1
				for k, capability := range model.Capabilities {
					path := fmt.Sprintf("%s.capabilities[%d]", modelPrefix, k)
					if _, known := knownProviderCapabilities[capability]; !known {
						return providerValidationError(path, "unknown capability")
					}
					if k > 0 && capability == model.Capabilities[k-1] {
						return providerValidationError(path, "duplicate capability")
					}
					if provider.Kind == ProviderKindAnthropic && (capability == "continuation" || capability == "reasoning_summary") {
						return providerValidationError(path, "capability is deferred for the Anthropic provider")
					}
					hasText = hasText || capability == "text"
					hasTools = hasTools || capability == "tools"
					if capability == "custom_tools" {
						customIndex = k
					}
					if capability == "namespace_tools" {
						namespaceIndex = k
					}
					if capability == "hosted_web_search" {
						hostedSearchIndex = k
					}
					if capability == "parallel_tools" {
						parallelIndex = k
					}
				}
				if customIndex >= 0 && !hasTools {
					path := fmt.Sprintf("%s.capabilities[%d]", modelPrefix, customIndex)
					return providerValidationError(path, "custom tools requires tools capability")
				}
				if hostedSearchIndex >= 0 && !hasTools {
					path := fmt.Sprintf("%s.capabilities[%d]", modelPrefix, hostedSearchIndex)
					return providerValidationError(path, "hosted web search requires tools capability")
				}
				if namespaceIndex >= 0 && !hasTools {
					path := fmt.Sprintf("%s.capabilities[%d]", modelPrefix, namespaceIndex)
					return providerValidationError(path, "namespace tools requires tools capability")
				}
				if parallelIndex >= 0 && !hasTools {
					path := fmt.Sprintf("%s.capabilities[%d]", modelPrefix, parallelIndex)
					return providerValidationError(path, "parallel tools requires tools capability")
				}
				if err := validateCodexModel(model.Codex, modelPrefix+".codex"); err != nil {
					return err
				}
				if provider.Kind == ProviderKindOpenAI && model.Codex != nil && model.Codex.Enabled && (!hasText || !hasTools || customIndex < 0 || namespaceIndex < 0 || hostedSearchIndex < 0 || parallelIndex < 0) {
					return providerValidationError(modelPrefix+".capabilities", "Codex-enabled OpenAI model requires the complete Codex capability baseline")
				}
				if provider.Kind == ProviderKindAnthropic {
					if !hasText {
						return providerValidationError(modelPrefix+".capabilities", "Anthropic models require text capability")
					}
					if model.MaxOutputTokens <= 0 || model.MaxOutputTokens > canonical.MaxOutputTokens {
						return providerValidationError(modelPrefix+".max_output_tokens", "invalid maximum output tokens")
					}
					if model.DefaultMaxOutputTokens <= 0 || model.DefaultMaxOutputTokens > model.MaxOutputTokens {
						return providerValidationError(modelPrefix+".default_max_output_tokens", "invalid default output tokens")
					}
				} else if model.DefaultMaxOutputTokens != 0 || model.MaxOutputTokens != 0 {
					return providerValidationError(modelPrefix+".default_max_output_tokens", "token limits are unavailable for the OpenAI provider")
				}
			}
		default:
			return providerValidationError(prefix+".kind", "unsupported provider kind")
		}
	}
	return nil
}

func validProviderID(value string) bool {
	return len(value) <= MaxProviderIDBytes && providerIDPattern.MatchString(value)
}

func validModelIdentifier(value string) bool {
	if len(value) == 0 || len(value) > MaxModelIDBytes || value[0] == '/' || value[len(value)-1] == '/' || strings.Contains(value, "//") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." || segment == "" {
			return false
		}
		for _, r := range segment {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
				return false
			}
		}
	}
	return true
}

func providerValidationError(path, message string) error {
	return &Error{Code: CodeValidationFailed, Path: path, Err: fmt.Errorf("%s", message)}
}
