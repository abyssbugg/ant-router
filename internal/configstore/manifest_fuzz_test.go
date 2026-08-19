package configstore

import (
	"reflect"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/revision"
)

func FuzzManifestRoundTrip(f *testing.F) {
	valid, err := EncodeManifest(Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("current")),
		ActivatedAt:   time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		f.Fatalf("EncodeManifest seed failed: %v", err)
	}
	f.Add(valid)
	f.Add([]byte("{}"))
	f.Add([]byte("not json"))
	f.Add([]byte(`{"generation":1,"generation":2}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		manifest, err := DecodeManifest(data)
		if err != nil {
			return
		}
		encoded, err := EncodeManifest(manifest)
		if err != nil {
			t.Fatalf("EncodeManifest failed after successful decode: %v", err)
		}
		roundTrip, err := DecodeManifest(encoded)
		if err != nil {
			t.Fatalf("encoded manifest failed to decode: %v", err)
		}
		if !reflect.DeepEqual(roundTrip, manifest) {
			t.Fatalf("manifest round trip changed value: got %#v, want %#v", roundTrip, manifest)
		}
	})
}
