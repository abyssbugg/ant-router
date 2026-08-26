package canonical

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func FuzzDecodeRequest(f *testing.F) {
	seed, err := os.ReadFile("testdata/request_continuation.json")
	if err != nil {
		f.Fatalf("read seed: %v", err)
	}
	f.Add(seed)
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := DecodeRequest(data)
		if err != nil {
			return
		}
		encoded, err := EncodeRequest(value)
		if err != nil {
			t.Fatalf("EncodeRequest: %v", err)
		}
		round, err := DecodeRequest(encoded)
		if err != nil {
			t.Fatalf("round decode: %v", err)
		}
		reencoded, err := EncodeRequest(round)
		if err != nil {
			t.Fatalf("round encode: %v", err)
		}
		if !bytes.Equal(encoded, reencoded) {
			t.Fatal("request round trip unstable")
		}
		_, _ = FullDigest(value)
		_, _ = PortableDigest(value)
	})
}

func FuzzDecodeResponse(f *testing.F) {
	seed, err := os.ReadFile("testdata/response_complete.json")
	if err != nil {
		f.Fatalf("read seed: %v", err)
	}
	f.Add(seed)
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := DecodeResponse(data)
		if err != nil {
			return
		}
		encoded, err := EncodeResponse(value)
		if err != nil {
			t.Fatalf("EncodeResponse: %v", err)
		}
		round, err := DecodeResponse(encoded)
		if err != nil {
			t.Fatalf("round decode: %v", err)
		}
		reencoded, err := EncodeResponse(round)
		if err != nil {
			t.Fatalf("round encode: %v", err)
		}
		if !bytes.Equal(encoded, reencoded) {
			t.Fatal("response round trip unstable")
		}
	})
}

func FuzzDecodeEvent(f *testing.F) {
	stream, err := os.ReadFile("testdata/event_stream_complete.json")
	if err != nil {
		f.Fatalf("read seed: %v", err)
	}
	var events []json.RawMessage
	if err := json.Unmarshal(stream, &events); err != nil {
		f.Fatalf("decode seed: %v", err)
	}
	for _, event := range events {
		f.Add([]byte(event))
	}
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := DecodeEvent(data)
		if err != nil {
			return
		}
		encoded, err := EncodeEvent(value)
		if err != nil {
			t.Fatalf("EncodeEvent: %v", err)
		}
		round, err := DecodeEvent(encoded)
		if err != nil {
			t.Fatalf("round decode: %v", err)
		}
		reencoded, err := EncodeEvent(round)
		if err != nil {
			t.Fatalf("round encode: %v", err)
		}
		if !bytes.Equal(encoded, reencoded) {
			t.Fatal("event round trip unstable")
		}
	})
}

func FuzzContinuation(f *testing.F) {
	f.Add("openai", "model-a", []byte{1, 2, 3})
	f.Add("Bad Provider", "", []byte{})
	f.Fuzz(func(t *testing.T, provider, model string, data []byte) {
		value, err := NewContinuation(provider, model, data)
		if err != nil {
			return
		}
		if !value.Valid() || value.Provider() != provider || value.Model() != model || !bytes.Equal(value.Bytes(), data) {
			t.Fatal("continuation constructor inconsistent")
		}
	})
}
