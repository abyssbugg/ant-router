package control

import (
	"bytes"
	"testing"
)

func FuzzConfigRequestJSON(f *testing.F) {
	f.Add([]byte(`{"toml":"schema_version = 1\n"}`))
	f.Add([]byte(`{"toml":"x","extra":true}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var request ConfigApplyRequest
		if err := decodeRequestJSON(bytes.NewReader(data), &request); err != nil {
			return
		}
		if len(request.TOML) > int(MaxConfigRequestBody) {
			t.Fatal("decoder accepted impossible request size")
		}
	})
}
