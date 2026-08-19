package revision

import "testing"

func FuzzIDValidation(f *testing.F) {
	f.Add(string(For([]byte("valid"))))
	f.Add("")
	f.Add("ABCDEF")
	f.Add("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")

	f.Fuzz(func(t *testing.T, value string) {
		id := ID(value)
		if !id.Valid() {
			return
		}
		if len(value) != 64 {
			t.Fatalf("Valid accepted length %d", len(value))
		}
		for _, character := range value {
			if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
				t.Fatalf("Valid accepted non-lowercase-hex character %q", character)
			}
		}
	})
}
