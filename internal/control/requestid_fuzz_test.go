package control

import "testing"

func FuzzValidRequestID(f *testing.F) {
	f.Add("request-123")
	f.Add("line\nbreak")
	f.Fuzz(func(t *testing.T, value string) {
		if !ValidRequestID(value) {
			return
		}
		if len(value) < 1 || len(value) > MaxRequestIDLength {
			t.Fatalf("accepted invalid request ID length")
		}
		for _, character := range value {
			if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.') {
				t.Fatalf("accepted invalid character %q", character)
			}
		}
	})
}
