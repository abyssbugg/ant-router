package config

import (
	"bytes"
	"reflect"
	"testing"
)

func FuzzDecodeCanonicalRoundTrip(f *testing.F) {
	f.Add([]byte("schema_version = 1\n"))
	f.Add([]byte("schema_version = 1\n[daemon]\nshutdown_timeout = \"30s\"\n"))
	f.Add([]byte("not valid TOML"))
	f.Add([]byte("schema_version = 2\n"))
	f.Add([]byte("schema_version = 5\n"))
	f.Add([]byte("schema_version = 6\n"))

	f.Fuzz(func(t *testing.T, source []byte) {
		cfg, err := Decode(bytes.NewReader(source))
		if err != nil {
			return
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Decode returned invalid configuration: %v", err)
		}
		canonical, err := Canonical(cfg)
		if err != nil {
			t.Fatalf("Canonical failed after successful Decode: %v", err)
		}
		roundTrip, err := Decode(bytes.NewReader(canonical))
		if err != nil {
			t.Fatalf("canonical bytes failed to decode: %v", err)
		}
		if !reflect.DeepEqual(roundTrip, cfg) {
			t.Fatalf("round trip changed configuration: got %#v, want %#v", roundTrip, cfg)
		}
	})
}
