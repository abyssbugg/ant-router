package responses

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func FuzzDecodeRequest(f *testing.F) {
	f.Add([]byte(`{"model":"mock/text","input":"hello"}`))
	f.Add([]byte(`{"model":"mock/text","input":[{"role":"user","content":"hello"}]}`))
	f.Add([]byte(`{"model":"mock/tool","input":"x","tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		parsed, err := Decode(bytes.NewReader(data), &sequenceIDs{})
		if err == nil && !parsed.Request.Valid() {
			t.Fatal("decoder returned an invalid canonical request")
		}
	})
}

func FuzzDecodeRequestRedactsUnknownValues(f *testing.F) {
	f.Add([]byte("private"))
	f.Add([]byte{0, 1, 2, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		marker := "sensitive-" + hex.EncodeToString(data)
		body, err := json.Marshal(map[string]any{"model": "mock/text", "input": "hello", "unknown": marker})
		if err != nil {
			t.Fatal(err)
		}
		_, decodeErr := Decode(bytes.NewReader(body), &sequenceIDs{})
		if CodeOf(decodeErr) != CodeUnknownField || PathOf(decodeErr) != "unknown" {
			t.Fatalf("unexpected error: %v", decodeErr)
		}
		if strings.Contains(decodeErr.Error(), marker) {
			t.Fatal("decoder error leaked an unknown field value")
		}
	})
}
