package codexownership

import "testing"

func FuzzDecodeStrict(f *testing.F) {
	f.Add([]byte(`{"schema_version":1,"current":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","profile_path":"/profile","catalog_path":"/catalog"}`))
	f.Add([]byte(`{"current":"x","current":"x"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxManifestBytes {
			return
		}
		var manifest Manifest
		_ = decodeStrict(data, &manifest)
		var generation Generation
		_ = decodeStrict(data, &generation)
	})
}
