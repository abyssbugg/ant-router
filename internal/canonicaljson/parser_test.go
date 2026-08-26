package canonicaljson

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseRejectsInvalidInputs(t *testing.T) {
	invalidUTF8 := []byte{'"', 0xff, '"'}
	if utf8.Valid(invalidUTF8) {
		t.Fatal("fixture unexpectedly valid UTF-8")
	}
	tests := []struct {
		name  string
		input []byte
		code  ErrorCode
	}{
		{"empty", nil, CodeInvalidJSON},
		{"syntax", []byte(`{"a":`), CodeInvalidJSON},
		{"trailing", []byte(`{} {}`), CodeInvalidJSON},
		{"duplicate root", []byte(`{"a":1,"a":2}`), CodeDuplicateKey},
		{"duplicate nested", []byte(`{"a":{"b":1,"b":2}}`), CodeDuplicateKey},
		{"utf8", invalidUTF8, CodeInvalidUTF8},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(test.input, DefaultLimits())
			if code, ok := CodeOf(err); !ok || code != test.code {
				t.Fatalf("code = %q, present = %v, want %q; error: %v", code, ok, test.code, err)
			}
		})
	}
}

func TestParseDepthBoundary(t *testing.T) {
	makeNested := func(depth int) []byte {
		return []byte(strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth))
	}
	limits := Limits{MaximumBytes: MaxDefaultBytes, MaximumDepth: 64}
	if _, err := Parse(makeNested(64), limits); err != nil {
		t.Fatalf("depth 64 rejected: %v", err)
	}
	if _, err := Parse(makeNested(65), limits); codeValue(err) != CodeLimitExceeded {
		t.Fatalf("depth 65 code = %q, want %q", codeValue(err), CodeLimitExceeded)
	}
}

func TestParseSizeBoundary(t *testing.T) {
	limits := Limits{MaximumBytes: 7, MaximumDepth: 8}
	if _, err := Parse([]byte(`{"a":1}`), limits); err != nil {
		t.Fatalf("7-byte JSON rejected: %v", err)
	}
	if _, err := Parse([]byte(`{"a":10}`), limits); codeValue(err) != CodeLimitExceeded {
		t.Fatalf("oversized code = %q", codeValue(err))
	}
}

func TestErrorsDoNotEchoKeysOrValues(t *testing.T) {
	canaryKey := "synthetic_secret_key_12345"
	canaryValue := "synthetic_secret_value_67890"
	input := []byte(`{"` + canaryKey + `":"` + canaryValue + `","` + canaryKey + `":null}`)
	_, err := Parse(input, DefaultLimits())
	if err == nil {
		t.Fatal("duplicate key accepted")
	}
	message := err.Error()
	if bytes.Contains([]byte(message), []byte(canaryKey)) || bytes.Contains([]byte(message), []byte(canaryValue)) {
		t.Errorf("error leaked JSON content: %q", message)
	}
}

func codeValue(err error) ErrorCode {
	code, _ := CodeOf(err)
	return code
}
