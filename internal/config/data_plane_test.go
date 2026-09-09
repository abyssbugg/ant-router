package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDefaultDataPlaneConfiguration(t *testing.T) {
	cfg := Default()
	if cfg.SchemaVersion != 6 {
		t.Fatalf("schema version = %d, want 6", cfg.SchemaVersion)
	}
	if cfg.DataPlane.Enabled || cfg.MockProvider.Enabled || len(cfg.Providers) != 0 {
		t.Fatal("data plane and providers must default to disabled")
	}
	if cfg.DataPlane.Listen != "127.0.0.1:8787" {
		t.Fatalf("listen = %q, want loopback default", cfg.DataPlane.Listen)
	}
	if cfg.DataPlane.MaxConcurrentRequests != 32 {
		t.Fatalf("concurrency = %d, want 32", cfg.DataPlane.MaxConcurrentRequests)
	}
	if cfg.DataPlane.ShutdownTimeout.Duration != 30*time.Second {
		t.Fatalf("shutdown timeout = %v, want 30s", cfg.DataPlane.ShutdownTimeout.Duration)
	}
}

func TestDecodeSchemaV2DataPlane(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`schema_version = 2
[data_plane]
enabled = true
listen = "127.0.0.1:9876"
max_concurrent_requests = 64
shutdown_timeout = "45s"
[mock_provider]
enabled = true
`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !cfg.DataPlane.Enabled || !cfg.MockProvider.Enabled || len(cfg.Providers) != 1 || cfg.Providers[0].Kind != ProviderKindMock {
		t.Fatal("enabled schema v2 mock provider was not migrated")
	}
	if cfg.DataPlane.Listen != "127.0.0.1:9876" || cfg.DataPlane.MaxConcurrentRequests != 64 {
		t.Fatalf("data plane = %+v", cfg.DataPlane)
	}
	if cfg.DataPlane.ShutdownTimeout.Duration != 45*time.Second {
		t.Fatalf("shutdown timeout = %v, want 45s", cfg.DataPlane.ShutdownTimeout.Duration)
	}
}

func TestDataPlaneValidation(t *testing.T) {
	valid := Default()
	valid.DataPlane.Enabled = true
	valid.Providers = []ProviderConfig{{ID: "mock", Kind: ProviderKindMock, Enabled: true}}

	tests := []struct {
		name string
		edit func(*Config)
		path string
	}{
		{"wildcard", func(c *Config) { c.DataPlane.Listen = "0.0.0.0:8787" }, "data_plane.listen"},
		{"hostname", func(c *Config) { c.DataPlane.Listen = "localhost:8787" }, "data_plane.listen"},
		{"ipv6", func(c *Config) { c.DataPlane.Listen = "[::1]:8787" }, "data_plane.listen"},
		{"port zero", func(c *Config) { c.DataPlane.Listen = "127.0.0.1:0" }, "data_plane.listen"},
		{"missing port", func(c *Config) { c.DataPlane.Listen = "127.0.0.1" }, "data_plane.listen"},
		{"zero concurrency", func(c *Config) { c.DataPlane.MaxConcurrentRequests = 0 }, "data_plane.max_concurrent_requests"},
		{"excess concurrency", func(c *Config) { c.DataPlane.MaxConcurrentRequests = 1025 }, "data_plane.max_concurrent_requests"},
		{"short shutdown", func(c *Config) { c.DataPlane.ShutdownTimeout = Duration{500 * time.Millisecond} }, "data_plane.shutdown_timeout"},
		{"long shutdown", func(c *Config) { c.DataPlane.ShutdownTimeout = Duration{6 * time.Minute} }, "data_plane.shutdown_timeout"},
		{"providers disabled", func(c *Config) { c.Providers[0].Enabled = false }, "providers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.edit(&cfg)
			err := cfg.Validate()
			var configErr *Error
			if !errors.As(err, &configErr) || configErr.Path != tt.path {
				t.Fatalf("Validate() error = %v, want path %q", err, tt.path)
			}
		})
	}
}

func TestValidateForInstanceRequiresDistinctNamedPort(t *testing.T) {
	cfg := Default()
	cfg.DataPlane.Enabled = true
	cfg.Providers = []ProviderConfig{{ID: "mock", Kind: ProviderKindMock, Enabled: true}}
	if err := cfg.ValidateForInstance("default"); err != nil {
		t.Fatalf("default instance: %v", err)
	}
	if err := cfg.ValidateForInstance("dev"); err == nil {
		t.Fatal("named instance accepted default data-plane port")
	}
	cfg.DataPlane.Listen = "127.0.0.1:8788"
	if err := cfg.ValidateForInstance("dev"); err != nil {
		t.Fatalf("named instance with distinct port: %v", err)
	}
}

func TestLegacyDirectConfigNormalizesDataPlane(t *testing.T) {
	legacy := Config{SchemaVersion: SchemaVersion, Daemon: DaemonConfig{ShutdownTimeout: Duration{30 * time.Second}}}
	candidate, err := NewCandidate(legacy, "", 0)
	if err != nil {
		t.Fatalf("NewCandidate() error = %v", err)
	}
	if candidate.Config().DataPlane != Default().DataPlane {
		t.Fatalf("candidate data plane = %+v, want defaults", candidate.Config().DataPlane)
	}
}

func TestCanonicalIncludesDataPlaneSections(t *testing.T) {
	canonical, err := Canonical(Default())
	if err != nil {
		t.Fatal(err)
	}
	text := string(canonical)
	for _, want := range []string{"schema_version = 6", "[data_plane]", "listen = '127.0.0.1:8787'"} {
		if !strings.Contains(text, want) {
			t.Fatalf("canonical config missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "mock_provider") {
		t.Fatalf("canonical config contains legacy mock_provider: %s", text)
	}
}
