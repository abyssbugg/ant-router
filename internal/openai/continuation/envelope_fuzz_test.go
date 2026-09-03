package continuation

import (
	"bytes"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func FuzzContinuationEnvelope(f *testing.F) {
	id, err := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
	if err != nil {
		f.Fatal(err)
	}
	seed, err := encodeContinuationEnvelope("openai-primary", "gpt-5.2", []continuationEntry{{canonicalItemID: id, upstreamItemID: "rs_seed", encryptedContent: "cipher-seed"}})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed.Bytes())
	f.Add([]byte(`{"entries":[],"version":1}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) == 0 || len(raw) > canonical.MaxContinuationBytes {
			t.Skip()
		}
		continuation, err := canonical.NewContinuation("openai-primary", "gpt-5.2", raw)
		if err != nil {
			return
		}
		envelope, err := decodeContinuationEnvelope(continuation, "openai-primary", "gpt-5.2", nil)
		if err != nil {
			return
		}
		encoded, err := encodeContinuationEnvelope("openai-primary", "gpt-5.2", envelope.entriesCopy())
		if err != nil {
			t.Fatalf("valid envelope failed to encode: %v", err)
		}
		if !bytes.Equal(encoded.Bytes(), continuation.Bytes()) {
			t.Fatal("valid envelope did not round trip deterministically")
		}
	})
}
