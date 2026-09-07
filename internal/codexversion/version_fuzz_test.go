package codexversion

import "testing"

func FuzzParse(f *testing.F) {
	f.Add("codex-cli 0.153.4")
	f.Add("secret")
	f.Fuzz(func(t *testing.T, value string) {
		version, err := Parse(value)
		if err != nil {
			return
		}
		if version.String() == "" {
			t.Fatal("parsed version has empty string")
		}
		if reparsed, err := Parse("codex-cli " + version.String()); err != nil || reparsed != version {
			t.Fatalf("round trip = %#v, %v", reparsed, err)
		}
	})
}
