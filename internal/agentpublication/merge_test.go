package agentpublication

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func sampleInput() PublicationInput {
	return PublicationInput{BaseURL: "http://127.0.0.1:8787/v1", APIKey: "token-value", Model: "deepseek-chat", ProviderName: "ant-router"}
}

func TestMergeGeminiIntoEmptyDocument(t *testing.T) {
	merged, err := MergeGeminiSettings(nil, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(merged, &document); err != nil {
		t.Fatal(err)
	}
	if document["modelProvider"] != "ant-router" {
		t.Fatalf("modelProvider = %v", document["modelProvider"])
	}
	providers, ok := document["providers"].(map[string]any)
	if !ok {
		t.Fatalf("providers = %#v", document["providers"])
	}
	entry, ok := providers["ant-router"].(map[string]any)
	if !ok {
		t.Fatalf("entry = %#v", providers["ant-router"])
	}
	if entry["baseUrl"] != "http://127.0.0.1:8787/v1" || entry["model"] != "deepseek-chat" || entry["type"] != "openai-compatible" {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestMergeGeminiPreservesUserKeysAndValues(t *testing.T) {
	original := []byte(`{
		"theme": "auto",
		"mcpServers": {"github": {"command": "gh-mcp"}},
		"nested": {"deep": {"value": 42}},
		"providers": {"other": {"name": "Other", "type": "openai-compatible", "baseUrl": "https://x.example/v1", "apiKey": "k", "model": "m"}}
	}`)
	merged, err := MergeGeminiSettings(original, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(merged, &document); err != nil {
		t.Fatal(err)
	}
	if document["theme"] != "auto" {
		t.Fatal("user key theme lost")
	}
	nested, _ := document["nested"].(map[string]any)
	deep, _ := nested["deep"].(map[string]any)
	if deep["value"] != float64(42) {
		t.Fatalf("nested value lost: %#v", nested)
	}
	providers := document["providers"].(map[string]any)
	if _, ok := providers["other"]; !ok {
		t.Fatal("existing other provider lost")
	}
	if _, ok := providers["ant-router"]; !ok {
		t.Fatal("ant-router entry missing")
	}
}

func TestMergeGeminiIsDeterministic(t *testing.T) {
	original := []byte(`{"theme":"auto","count":3}`)
	first, err := MergeGeminiSettings(original, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	second, err := MergeGeminiSettings(original, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("merge is not deterministic")
	}
}

func TestMergeGeminiIdempotent(t *testing.T) {
	first, err := MergeGeminiSettings(nil, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	second, err := MergeGeminiSettings(first, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("republish not idempotent:\nfirst  %s\nsecond %s", first, second)
	}
}

func TestMergeGeminiRejectsDriftedEntry(t *testing.T) {
	original := []byte(`{"providers":{"ant-router":{"name":"Ant Router","type":"openai-compatible","baseUrl":"http://127.0.0.1:9999/v1","apiKey":"old","model":"old-model"}}}`)
	_, err := MergeGeminiSettings(original, sampleInput())
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("error = %v, want ErrDrift", err)
	}
}

func TestMergeGeminiAcceptsOwnedEntryUnchanged(t *testing.T) {
	first, err := MergeGeminiSettings(nil, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	// Republishing over an owned file must succeed (idempotence covers this,
	// but the drift check must not false-positive on its own entry).
	second, err := MergeGeminiSettings(first, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("owned republish changed bytes")
	}
}

func TestMergeGeminiRejectsUnrepresentable(t *testing.T) {
	if _, err := MergeGeminiSettings([]byte(`{"theme": "auto", // comment`), sampleInput()); !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("comment file error = %v, want ErrUnrepresentable", err)
	}
	if _, err := MergeGeminiSettings([]byte(`[1,2,3]`), sampleInput()); !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("array settings error = %v, want ErrUnrepresentable", err)
	}
	if _, err := MergeGeminiSettings([]byte(`{"providers": [1]}`), sampleInput()); !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("providers array error = %v, want ErrUnrepresentable", err)
	}
}

func TestMergeGeminiValidatesInput(t *testing.T) {
	for _, mutate := range []func(*PublicationInput){
		func(i *PublicationInput) { i.BaseURL = "" },
		func(i *PublicationInput) { i.APIKey = "" },
		func(i *PublicationInput) { i.Model = "" },
		func(i *PublicationInput) { i.ProviderName = "bad name!" },
	} {
		input := sampleInput()
		mutate(&input)
		if _, err := MergeGeminiSettings(nil, input); err == nil {
			t.Fatalf("accepted invalid input %+v", input)
		}
	}
}

func TestExtractGeminiEntry(t *testing.T) {
	if _, found, err := ExtractGeminiEntry(nil, "ant-router"); err != nil || found {
		t.Fatalf("empty settings = found %t err %v", found, err)
	}
	merged, err := MergeGeminiSettings(nil, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	entry, found, err := ExtractGeminiEntry(merged, "ant-router")
	if err != nil || !found {
		t.Fatalf("found = %t err = %v", found, err)
	}
	if !strings.Contains(string(entry), "deepseek-chat") {
		t.Fatalf("entry = %s", entry)
	}
	if _, found, err := ExtractGeminiEntry([]byte(`{"providers":{"other":{}}}`), "ant-router"); err != nil || found {
		t.Fatalf("missing entry = found %t err %v", found, err)
	}
}

func TestMergeGeminiToleratesTopLevelNull(t *testing.T) {
	merged, err := MergeGeminiSettings([]byte(`null`), sampleInput())
	if err != nil {
		t.Fatalf("top-level null rejected: %v", err)
	}
	if !strings.Contains(string(merged), `"modelProvider":"ant-router"`) && !strings.Contains(string(merged), `"modelProvider": "ant-router"`) {
		t.Fatalf("merged = %s", merged)
	}
}

func TestMergeGeminiPreservesLargeNumbers(t *testing.T) {
	original := []byte(`{"bigNumber":9007199254740993}`)
	merged, err := MergeGeminiSettings(original, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), "9007199254740993") {
		t.Fatalf("large number corrupted: %s", merged)
	}
}

func TestMergeGeminiAllowsOwnedStaleEntryAfterRotation(t *testing.T) {
	first, err := MergeGeminiSettings(nil, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	rotated := sampleInput()
	rotated.APIKey = "rotated-token"
	second, err := MergeGeminiSettingsWithOwned(first, first, rotated)
	if err != nil {
		t.Fatalf("owned stale entry rejected after rotation: %v", err)
	}
	if !strings.Contains(string(second), "rotated-token") {
		t.Fatalf("rotated token missing: %s", second)
	}
	// A foreign entry still drifts.
	if _, err := MergeGeminiSettingsWithOwned([]byte(`{"providers":{"ant-router":{"name":"Ant Router","type":"openai-compatible","baseUrl":"http://evil.example/v1","apiKey":"k","model":"m"}}}`), first, rotated); !errors.Is(err, ErrDrift) {
		t.Fatalf("foreign entry error = %v, want ErrDrift", err)
	}
}
