package control

import (
	"testing"
	"time"
)

func TestRuntimeStatusAcceptsAnthropicProviderSummary(t *testing.T) {
	status := RuntimeStatus{
		Instance: "default", DaemonState: "running", StartedAt: time.Unix(1_700_000_000, 0).UTC(),
		Providers: []ProviderSummary{{ID: "anthropic-primary", Kind: "anthropic", Available: true}},
	}
	if err := status.Validate(); err != nil {
		t.Fatalf("Anthropic provider status rejected: %v", err)
	}
}
