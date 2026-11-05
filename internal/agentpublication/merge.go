// Package agentpublication publishes the router's model endpoint to local AI
// CLI agents. It is separate from the Codex publication stack: the Codex
// pipeline writes only ant-router-owned files, while this package merges into
// agent-owned settings files, so it carries its own pre-image capture and
// restore semantics.
package agentpublication

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// PublicationInput describes what the router advertises to an agent.
type PublicationInput struct {
	// BaseURL is the data plane origin, e.g. http://127.0.0.1:8787/v1.
	BaseURL string
	// APIKey is the inference credential the agent will present.
	APIKey string
	// Model is the default public model alias the agent selects.
	Model string
	// ProviderName is the settings key for the router entry.
	ProviderName string
}

const defaultProviderName = "ant-router"

var (
	// ErrDrift reports an existing ant-router entry that does not match the
	// owned generation; the user must resolve it before publishing.
	ErrDrift = errors.New("agent settings drifted from the owned publication")
	// ErrUnrepresentable reports settings that cannot be merged safely.
	ErrUnrepresentable = errors.New("agent settings cannot be merged safely")
)

// MergeGeminiSettings returns the merged settings.json bytes: every key of
// the original document is preserved, and the ant-router provider entry plus
// the modelProvider/model selection are set to the publication input.
// original may be nil (first publication). The result is deterministic for
// identical inputs.
func MergeGeminiSettings(original []byte, input PublicationInput) ([]byte, error) {
	return MergeGeminiSettingsWithOwned(original, nil, input)
}

// MergeGeminiSettingsWithOwned additionally treats an existing ant-router
// entry matching ownedPrior (the previously published merged settings, from
// the store) as owned rather than drifted, so re-publication after token
// rotation succeeds without a manual force flag.
func MergeGeminiSettingsWithOwned(original, ownedPrior []byte, input PublicationInput) ([]byte, error) {
	if input.BaseURL == "" {
		return nil, errors.New("base URL is required")
	}
	if input.APIKey == "" {
		return nil, errors.New("API key is required")
	}
	if input.Model == "" {
		return nil, errors.New("default model is required")
	}
	name := input.ProviderName
	if name == "" {
		name = defaultProviderName
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	document := map[string]any{}
	if len(original) > 0 {
		if !json.Valid(original) {
			return nil, fmt.Errorf("%w: existing settings are not valid JSON (comments are not supported)", ErrUnrepresentable)
		}
		decoder := json.NewDecoder(bytes.NewReader(original))
		decoder.UseNumber()
		if err := decoder.Decode(&document); err != nil {
			return nil, fmt.Errorf("%w: existing settings are not a JSON object", ErrUnrepresentable)
		}
		if document == nil {
			// A top-level JSON null unmarshals to a nil map; treat it as an
			// empty document rather than panicking on assignment.
			document = map[string]any{}
		}
	}
	provider := map[string]any{
		"name":    "Ant Router",
		"type":    "openai-compatible",
		"baseUrl": input.BaseURL,
		"apiKey":  input.APIKey,
		"model":   input.Model,
	}
	if existing, ok := document["providers"]; ok {
		providers, ok := existing.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: providers is not an object", ErrUnrepresentable)
		}
		if existingEntry, ok := providers[name]; ok {
			existingBytes, err := json.Marshal(existingEntry)
			if err != nil {
				return nil, fmt.Errorf("%w: existing %s entry cannot be inspected", ErrDrift, name)
			}
			desiredBytes, err := json.Marshal(provider)
			if err != nil {
				return nil, fmt.Errorf("%w: desired entry cannot be rendered", ErrDrift)
			}
			if string(existingBytes) != string(desiredBytes) {
				if !entryMatchesOwned(existingBytes, ownedPrior, name) {
					return nil, fmt.Errorf("%w: existing %s entry differs from the publication", ErrDrift, name)
				}
			}
		}
		providers[name] = provider
		document["providers"] = providers
	} else {
		document["providers"] = map[string]any{name: provider}
	}
	document["modelProvider"] = name
	merged, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: merged settings cannot be rendered", ErrUnrepresentable)
	}
	return append(merged, '\n'), nil
}

// ExtractGeminiEntry reports whether the settings document carries an
// ant-router provider entry and returns its rendered bytes for drift checks.
func ExtractGeminiEntry(settings []byte, providerName string) ([]byte, bool, error) {
	if len(settings) == 0 {
		return nil, false, nil
	}
	document := map[string]json.RawMessage{}
	if err := json.Unmarshal(settings, &document); err != nil {
		return nil, false, fmt.Errorf("%w: existing settings are not valid JSON", ErrUnrepresentable)
	}
	providersRaw, ok := document["providers"]
	if !ok {
		return nil, false, nil
	}
	var providers map[string]json.RawMessage
	if err := json.Unmarshal(providersRaw, &providers); err != nil {
		return nil, false, fmt.Errorf("%w: providers is not an object", ErrUnrepresentable)
	}
	entry, ok := providers[providerName]
	if !ok {
		return nil, false, nil
	}
	return entry, true, nil
}

// entryMatchesOwned reports whether the existing entry equals the ant-router
// entry inside the prior owned merged settings, meaning the on-disk state is
// the router's own stale publication (safe to republish, e.g. after token
// rotation).
func entryMatchesOwned(existingBytes []byte, ownedPrior []byte, name string) bool {
	if len(ownedPrior) == 0 {
		return false
	}
	var priorDocument map[string]json.RawMessage
	if err := json.Unmarshal(ownedPrior, &priorDocument); err != nil {
		return false
	}
	providersRaw, ok := priorDocument["providers"]
	if !ok {
		return false
	}
	var providers map[string]json.RawMessage
	if err := json.Unmarshal(providersRaw, &providers); err != nil {
		return false
	}
	priorEntry, ok := providers[name]
	if !ok {
		return false
	}
	var existingValue, priorValue any
	if err := json.Unmarshal(existingBytes, &existingValue); err != nil {
		return false
	}
	if err := json.Unmarshal(priorEntry, &priorValue); err != nil {
		return false
	}
	existingCanonical, err := json.Marshal(existingValue)
	if err != nil {
		return false
	}
	priorCanonical, err := json.Marshal(priorValue)
	if err != nil {
		return false
	}
	return string(existingCanonical) == string(priorCanonical)
}

func validateName(name string) error {
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return fmt.Errorf("%w: provider name %q contains unsupported characters", ErrUnrepresentable, name)
		}
	}
	return nil
}
