package canonical

import (
	"bytes"
	"strings"
	"testing"
)

func TestCodecErrorsDoNotLeakCanaries(t *testing.T) {
	canaryKey := "synthetic_secret_key_12345"
	canaryValue := "synthetic_secret_value_67890"
	documents := [][]byte{
		[]byte(`{"version":"1","` + canaryKey + `":"` + canaryValue + `"}`),
		[]byte(`{"version":"1","id":"bad","model":"` + canaryValue + `","input":[]}`),
	}
	for _, document := range documents {
		_, err := DecodeRequest(document)
		if err == nil {
			t.Fatal("invalid request accepted")
		}
		if strings.Contains(err.Error(), canaryKey) || strings.Contains(err.Error(), canaryValue) {
			t.Errorf("error leaked canary: %q", err)
		}
	}
}

func TestCodecRejectsNestedDuplicateAndDepthBomb(t *testing.T) {
	duplicate := []byte(`{"version":"1","id":"x","model":"m","input":[{"type":"message","id":"x","role":"user","content":[{"type":"json","value":{"secret":1,"secret":2}}]}]}`)
	if _, err := DecodeRequest(duplicate); codeOfCanonical(err) != CodeDuplicateKey {
		t.Errorf("duplicate code = %q", codeOfCanonical(err))
	}
	deep := []byte(strings.Repeat("[", MaxJSONDepth+1) + "0" + strings.Repeat("]", MaxJSONDepth+1))
	document := append([]byte(`{"version":"1","extra":`), deep...)
	document = append(document, '}')
	if _, err := DecodeRequest(document); codeOfCanonical(err) != CodeLimitExceeded {
		t.Errorf("depth code = %q", codeOfCanonical(err))
	}
}

func TestPortableDigestIgnoresManyContinuationValues(t *testing.T) {
	base := codecRequest(t, 1)
	expected, err := PortableDigest(base)
	if err != nil {
		t.Fatalf("PortableDigest: %v", err)
	}
	for value := byte(2); value < 64; value++ {
		request := codecRequest(t, value)
		got, err := PortableDigest(request)
		if err != nil || got != expected {
			t.Fatalf("continuation %d changed portable digest", value)
		}
		full, _ := FullDigest(request)
		if bytes.Equal(full[:], expected[:]) {
			t.Fatalf("full digest omitted continuation %d", value)
		}
	}
}
