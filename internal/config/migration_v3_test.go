package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeMigratesSchemaV2MockProviderToV4(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`schema_version = 2
[data_plane]
enabled = true
listen = "127.0.0.1:9876"
[mock_provider]
enabled = true
`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if cfg.SchemaVersion != 5 {
		t.Fatalf("schema version = %d, want 5", cfg.SchemaVersion)
	}
	want := []ProviderConfig{{ID: "mock", Kind: ProviderKindMock, Enabled: true}}
	if !reflect.DeepEqual(cfg.Providers, want) {
		t.Fatalf("providers = %#v, want %#v", cfg.Providers, want)
	}
}

func TestDecodeMigratesDisabledSchemaV2MockProviderToNoProviders(t *testing.T) {
	cfg, err := Decode(strings.NewReader("schema_version = 2\n[mock_provider]\nenabled = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != 5 || len(cfg.Providers) != 0 {
		t.Fatalf("migrated config = %#v", cfg)
	}
}

func TestDecodeSchemaV3RejectsLegacyMockProviderField(t *testing.T) {
	_, err := Decode(strings.NewReader("schema_version = 3\n[mock_provider]\nenabled = true\n"))
	var configErr *Error
	if !errors.As(err, &configErr) || configErr.Code != CodeUnknownField || configErr.Path != "mock_provider" {
		t.Fatalf("Decode() error = %#v, want unknown mock_provider", err)
	}
}

func TestMigratedSchemaV2CanonicalizesAsV5(t *testing.T) {
	cfg, err := Decode(strings.NewReader("schema_version = 2\n[mock_provider]\nenabled = true\n"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := Canonical(cfg)
	if err != nil {
		t.Fatal(err)
	}
	text := string(canonical)
	if !strings.Contains(text, "schema_version = 5") || !strings.Contains(text, "[[providers]]") || strings.Contains(text, "mock_provider") {
		t.Fatalf("canonical migrated config = %s", canonical)
	}
	roundTrip, err := Decode(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundTrip, cfg) {
		t.Fatalf("round trip = %#v, want %#v", roundTrip, cfg)
	}
}

func TestSchemaV1MigratesDirectlyToV5(t *testing.T) {
	cfg, err := Decode(strings.NewReader("schema_version = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != 5 || len(cfg.Providers) != 0 {
		t.Fatalf("migrated config = %#v", cfg)
	}
}
