package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
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
	ProviderKindMock   ProviderKind = "mock"
	ProviderKindOpenAI ProviderKind = "openai"
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
	Alias        string   `toml:"alias"`
	Upstream     string   `toml:"upstream"`
	Capabilities []string `toml:"capabilities"`
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
	"image_input":       {},
	"parallel_tools":    {},
	"reasoning_summary": {},
	"structured_output": {},
	"text":              {},
	"tools":             {},
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

func validateProviders(c Config) error {
	if len(c.Providers) > MaxProviders {
		return providerValidationError("providers", "exceeds provider limit")
	}
	providerIDs := make(map[string]struct{}, len(c.Providers))
	aliases := make(map[string]struct{})
	for _, provider := range c.Providers {
		if provider.Kind == ProviderKindMock {
			for _, alias := range []string{"mock/disconnect", "mock/json", "mock/reasoning", "mock/slow", "mock/text", "mock/tool", "mock/vision"} {
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
			if len(provider.Models) != 0 {
				return providerValidationError(prefix+".models", "mock provider uses its fixed model catalog")
			}
		case ProviderKindOpenAI:
			if provider.Credential == nil {
				return providerValidationError(prefix+".credential", "credential reference is required")
			}
			if provider.Credential.Source != CredentialSourceEnv {
				return providerValidationError(prefix+".credential.source", "unsupported credential source")
			}
			if len(provider.Credential.Name) == 0 || len(provider.Credential.Name) > MaxCredentialName || !envNamePattern.MatchString(provider.Credential.Name) {
				return providerValidationError(prefix+".credential.name", "invalid environment variable name")
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
				hasTools := false
				parallelIndex := -1
				for k, capability := range model.Capabilities {
					path := fmt.Sprintf("%s.capabilities[%d]", modelPrefix, k)
					if _, known := knownProviderCapabilities[capability]; !known {
						return providerValidationError(path, "unknown capability")
					}
					if k > 0 && capability == model.Capabilities[k-1] {
						return providerValidationError(path, "duplicate capability")
					}
					if capability == "continuation" {
						return providerValidationError(path, "continuation is unavailable for the OpenAI provider")
					}
					hasTools = hasTools || capability == "tools"
					if capability == "parallel_tools" {
						parallelIndex = k
					}
				}
				if parallelIndex >= 0 && !hasTools {
					path := fmt.Sprintf("%s.capabilities[%d]", modelPrefix, parallelIndex)
					return providerValidationError(path, "parallel tools requires tools capability")
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
