package canonicaljson

import "testing"

func FuzzParseRoundTrip(f *testing.F) {
	f.Add([]byte(`{"b":2,"a":1}`))
	f.Add([]byte(`[1,1.0,true,null,"x"]`))
	// This seed intentionally exercises duplicate-key rejection.
	f.Add([]byte(`{"a":1,"a":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := Parse(data, DefaultLimits())
		if err != nil {
			return
		}
		roundTrip, err := Parse(value.Bytes(), DefaultLimits())
		if err != nil || !value.Equal(roundTrip) {
			t.Fatalf("canonical round trip failed: %v", err)
		}
	})
}
