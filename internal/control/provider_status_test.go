package control

import (
	"testing"
	"time"
)

func TestRuntimeStatusValidatesProviderSummaries(t *testing.T) {
	t.Parallel()
	base := RuntimeStatus{
		Instance:    "default",
		DaemonState: "running",
		StartedAt:   time.Date(2026, 8, 31, 1, 0, 0, 0, time.UTC),
		Providers: []ProviderSummary{
			{ID: "mock", Kind: "mock", Available: true},
			{ID: "openai-primary", Kind: "openai", Code: "authentication_failed", Reference: "OPENAI_API_KEY"},
		},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid status rejected: %v", err)
	}
	invalid := []struct {
		name      string
		providers []ProviderSummary
	}{
		{name: "unsorted", providers: []ProviderSummary{{ID: "z", Kind: "mock", Available: true}, {ID: "a", Kind: "openai", Available: true}}},
		{name: "duplicate", providers: []ProviderSummary{{ID: "a", Kind: "mock", Available: true}, {ID: "a", Kind: "openai", Available: true}}},
		{name: "unsafe id", providers: []ProviderSummary{{ID: "../openai", Kind: "openai", Available: true}}},
		{name: "unknown kind", providers: []ProviderSummary{{ID: "openai", Kind: "private", Available: true}}},
		{name: "available code", providers: []ProviderSummary{{ID: "openai", Kind: "openai", Available: true, Code: "unavailable"}}},
		{name: "unavailable missing code", providers: []ProviderSummary{{ID: "openai", Kind: "openai"}}},
		{name: "unknown code", providers: []ProviderSummary{{ID: "openai", Kind: "openai", Code: "private"}}},
		{name: "unsafe reference", providers: []ProviderSummary{{ID: "openai", Kind: "openai", Code: "authentication_failed", Reference: "secret value"}}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			status := base
			status.Providers = test.providers
			if err := status.Validate(); err == nil {
				t.Fatal("invalid provider status accepted")
			}
		})
	}
}

func TestBuildStatusResponseCopiesProviderSummaries(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 8, 31, 1, 0, 0, 0, time.UTC)
	providers := []ProviderSummary{{ID: "openai", Kind: "openai", Code: "authentication_failed", Reference: "OPENAI_API_KEY"}}
	response := buildStatusResponse(RuntimeStatus{Instance: "default", DaemonState: "running", StartedAt: started, Providers: providers}, started.Add(time.Second))
	providers[0].Reference = "CHANGED"
	if len(response.Providers) != 1 || response.Providers[0].Reference != "OPENAI_API_KEY" {
		t.Fatalf("Providers = %#v", response.Providers)
	}
	if err := validateStatusResponse(response); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	response.Providers[0].Reference = "private value"
	if err := validateStatusResponse(response); err == nil {
		t.Fatal("unsafe provider response accepted")
	}
}
