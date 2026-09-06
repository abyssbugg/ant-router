package config

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDecodeMigratesSchemaV1ToV4(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`schema_version = 1
[daemon]
shutdown_timeout = "45s"
`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if cfg.SchemaVersion != 4 {
		t.Fatalf("schema version = %d, want 4", cfg.SchemaVersion)
	}
	if cfg.Daemon.ShutdownTimeout.Duration != 45*time.Second {
		t.Fatalf("daemon timeout = %v, want 45s", cfg.Daemon.ShutdownTimeout.Duration)
	}
	if cfg.DataPlane.Enabled || cfg.MockProvider.Enabled {
		t.Fatal("migration enabled new services")
	}
	if cfg.DataPlane.Listen != "127.0.0.1:8787" {
		t.Fatalf("migrated listen = %q", cfg.DataPlane.Listen)
	}
}

func TestDecodeRejectsV1WithV2Sections(t *testing.T) {
	_, err := Decode(strings.NewReader(`schema_version = 1
[data_plane]
enabled = true
`))
	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Fatalf("Decode() error = %v, want config error", err)
	}
	if configErr.Code != CodeUnknownField || configErr.Path != "data_plane" {
		t.Fatalf("Decode() error = %#v, want unknown data_plane", configErr)
	}
}

func TestMigratedCanonicalIsSchemaV4(t *testing.T) {
	cfg, err := Decode(strings.NewReader("schema_version = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := Canonical(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(canonical), "schema_version = 4") {
		t.Fatalf("canonical migrated config = %s", canonical)
	}
	roundTrip, err := Decode(strings.NewReader(string(canonical)))
	if err != nil {
		t.Fatalf("Decode(canonical) error = %v", err)
	}
	if !reflect.DeepEqual(roundTrip, cfg) {
		t.Fatalf("round trip = %+v, want %+v", roundTrip, cfg)
	}
}

func TestDecodeRejectsUnsupportedSchemaAfterMigrationWindow(t *testing.T) {
	for _, version := range []int{0, 5, 99} {
		_, err := Decode(strings.NewReader("schema_version = " + strconv.Itoa(version) + "\n"))
		var configErr *Error
		if !errors.As(err, &configErr) || configErr.Code != CodeUnsupportedSchema {
			t.Fatalf("version %d error = %v, want unsupported schema", version, err)
		}
	}
}
