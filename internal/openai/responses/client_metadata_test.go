package responses

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestClientMetadataIsValidatedAndNonsemantic(t *testing.T) {
	base, err := Decode(strings.NewReader(`{"model":"mock/text","input":"hi"}`), &sequenceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	withMetadata, err := Decode(strings.NewReader(`{"model":"mock/text","input":"hi","client_metadata":{"ws_request_header_traceparent":"00-abcd-01","x-codex-turn-metadata":"{\"originator\":\"codex_cli_rs\"}"}}`), &sequenceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	baseDigest, err := canonical.PortableDigest(base.Request)
	if err != nil {
		t.Fatal(err)
	}
	metadataDigest, err := canonical.PortableDigest(withMetadata.Request)
	if err != nil {
		t.Fatal(err)
	}
	if baseDigest != metadataDigest {
		t.Fatal("client metadata changed canonical semantics")
	}
}

func TestClientMetadataRejectsInvalidAndOversizedValuesWithoutLeakage(t *testing.T) {
	secret := "metadata-secret-marker"
	entries := make([]string, maxClientMetadataEntries+1)
	for i := range entries {
		entries[i] = fmt.Sprintf(`"k%d":"v"`, i)
	}
	cases := []string{
		`null`, `[]`, `{"k":1}`, `{"":"v"}`,
		`{"k":"line\nbreak"}`,
		`{"` + strings.Repeat("k", maxClientMetadataKeyBytes+1) + `":"` + secret + `"}`,
		`{"k":"` + strings.Repeat("v", maxClientMetadataValueBytes+1) + secret + `"}`,
		`{` + strings.Join(entries, ",") + `}`,
	}
	for _, metadata := range cases {
		body := `{"model":"mock/text","input":"hi","client_metadata":` + metadata + `}`
		_, err := Decode(strings.NewReader(body), &sequenceIDs{})
		if err == nil {
			t.Fatalf("accepted client_metadata=%s", metadata[:min(len(metadata), 40)])
		}
		if PathOf(err) != "client_metadata" {
			t.Fatalf("path=%q", PathOf(err))
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "x-codex") {
			t.Fatal("metadata leaked")
		}
	}
}

func TestClientMetadataAcceptsMaximumSafeShape(t *testing.T) {
	metadata := `{"` + strings.Repeat("k", maxClientMetadataKeyBytes) + `":"` + strings.Repeat("v", maxClientMetadataValueBytes) + `"}`
	body := []byte(`{"model":"mock/text","input":"hi","client_metadata":` + metadata + `}`)
	if len(metadata) > maxClientMetadataBytes {
		t.Fatal("test metadata exceeds aggregate limit")
	}
	if _, err := Decode(bytes.NewReader(body), &sequenceIDs{}); err != nil {
		t.Fatal(err)
	}
}
