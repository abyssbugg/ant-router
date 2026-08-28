package inferencetoken

import "testing"

func FuzzParseToken(f *testing.F) {
	f.Add("")
	f.Add("malformed")
	f.Fuzz(func(t *testing.T, value string) {
		token, err := Parse(value)
		if err != nil {
			return
		}
		if len(value) != EncodedTokenLength || token.String() != value || !token.Verify(value) {
			t.Fatalf("Parse accepted inconsistent token")
		}
	})
}
