package canonical

import (
	"bytes"
	"encoding/base64"
	"io"
	"testing"
)

type idCase struct {
	name     string
	prefix   string
	generate func(io.Reader) (string, error)
	parse    func(string) (string, error)
}

func canonicalIDCases() []idCase {
	return []idCase{
		{"request", "req_", func(r io.Reader) (string, error) { id, err := GenerateRequestID(r); return id.String(), err }, func(v string) (string, error) { id, err := ParseRequestID(v); return id.String(), err }},
		{"response", "resp_", func(r io.Reader) (string, error) { id, err := GenerateResponseID(r); return id.String(), err }, func(v string) (string, error) { id, err := ParseResponseID(v); return id.String(), err }},
		{"item", "item_", func(r io.Reader) (string, error) { id, err := GenerateItemID(r); return id.String(), err }, func(v string) (string, error) { id, err := ParseItemID(v); return id.String(), err }},
		{"call", "call_", func(r io.Reader) (string, error) { id, err := GenerateToolCallID(r); return id.String(), err }, func(v string) (string, error) { id, err := ParseToolCallID(v); return id.String(), err }},
		{"event", "evt_", func(r io.Reader) (string, error) { id, err := GenerateEventID(r); return id.String(), err }, func(v string) (string, error) { id, err := ParseEventID(v); return id.String(), err }},
	}
}

func TestCanonicalIDGenerateAndParse(t *testing.T) {
	material := make([]byte, IDRandomBytes)
	for index := range material {
		material[index] = byte(index)
	}
	encoded := base64.RawURLEncoding.EncodeToString(material)
	for _, test := range canonicalIDCases() {
		t.Run(test.name, func(t *testing.T) {
			value, err := test.generate(bytes.NewReader(material))
			if err != nil {
				t.Fatalf("generate failed: %v", err)
			}
			want := test.prefix + encoded
			if value != want {
				t.Errorf("generated = %q, want %q", value, want)
			}
			parsed, err := test.parse(value)
			if err != nil || parsed != value {
				t.Errorf("parse = %q, %v", parsed, err)
			}
		})
	}
}

func TestCanonicalIDRejectsInvalidValues(t *testing.T) {
	validPayload := base64.RawURLEncoding.EncodeToString(make([]byte, IDRandomBytes))
	invalid := []string{"", "wrong_" + validPayload, "req_short", "req_" + validPayload + "=", "req_" + validPayload[:len(validPayload)-1] + "!"}
	for _, value := range invalid {
		if _, err := ParseRequestID(value); codeOfCanonical(err) != CodeInvalidID {
			t.Errorf("ParseRequestID(%q) code = %q", value, codeOfCanonical(err))
		}
	}
}

func TestCanonicalIDShortRandomReader(t *testing.T) {
	for _, test := range canonicalIDCases() {
		if _, err := test.generate(bytes.NewReader(make([]byte, IDRandomBytes-1))); codeOfCanonical(err) != CodeInvalidID {
			t.Errorf("%s short-reader code = %q", test.name, codeOfCanonical(err))
		}
	}
}

func codeOfCanonical(err error) ErrorCode {
	code, _ := CodeOf(err)
	return code
}
