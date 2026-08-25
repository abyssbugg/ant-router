package canonical

import "testing"

func FuzzParseRequestID(f *testing.F) {
	f.Add("req_AAAAAAAAAAAAAAAAAAAAAA")
	f.Add("wrong")
	f.Fuzz(func(t *testing.T, value string) {
		id, err := ParseRequestID(value)
		if err != nil {
			return
		}
		if !id.Valid() || id.String() != value {
			t.Fatalf("accepted inconsistent request ID %q", value)
		}
	})
}
