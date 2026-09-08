package codexintegration

import (
	"encoding/json"
	"reflect"
	"testing"
)

func FuzzDecodePending(f *testing.F) {
	f.Add([]byte(`{"schema_version":1,"action":"enable","target_generation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","profile_path":"/profile","catalog_path":"/catalog"}`))
	f.Add([]byte(`{"schema_version":1,"action":"disable","base_generation":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","profile_path":"/profile","catalog_path":"/catalog"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxPendingBytes+1 {
			return
		}
		pending, err := decodePending(data)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(pending)
		if err != nil {
			t.Fatal(err)
		}
		roundTrip, err := decodePending(encoded)
		if err != nil {
			t.Fatalf("round trip failed: %v", err)
		}
		if !reflect.DeepEqual(pending, roundTrip) {
			t.Fatalf("round trip mismatch: %#v %#v", pending, roundTrip)
		}
	})
}
