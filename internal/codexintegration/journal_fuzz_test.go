package codexintegration

import (
	"encoding/json"
	"reflect"
	"testing"
)

func FuzzDecodePending(f *testing.F) {
	f.Add([]byte(`{"schema_version":2,"action":"enable","target_generation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","profile_path":"/profile","catalog_path":"/catalog","base_profile_exists":false,"base_catalog_exists":false,"target_profile_exists":true,"target_catalog_exists":true}`))
	f.Add([]byte(`{"schema_version":2,"action":"disable","base_generation":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","profile_path":"/profile","catalog_path":"/catalog","base_profile_exists":true,"base_catalog_exists":true,"target_profile_exists":false,"target_catalog_exists":false}`))
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
