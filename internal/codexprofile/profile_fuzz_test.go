package codexprofile

import (
	"bytes"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func FuzzRender(f *testing.F) {
	f.Add("http://127.0.0.1:8787/v1", "/catalog", "/routerctl", "default", "/state", "/run")
	f.Add("secret", "relative", "relative", "../bad", "relative", "relative")
	f.Fuzz(func(t *testing.T, baseURL, catalog, command, instance, state, runtime string) {
		options := Options{BaseURL: baseURL, CatalogPath: catalog, RouterctlPath: command, Instance: instance, StateRoot: state, RuntimeRoot: runtime}
		first, err := Render(options)
		if err != nil {
			return
		}
		second, err := Render(options)
		if err != nil || !bytes.Equal(first, second) {
			t.Fatal("profile rendering is not deterministic")
		}
		var decoded map[string]any
		if err := toml.Unmarshal(first, &decoded); err != nil {
			t.Fatalf("rendered profile is invalid TOML: %v", err)
		}
	})
}
