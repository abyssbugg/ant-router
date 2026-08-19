package controltoken

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	material := make([]byte, TokenSize)
	for index := range material {
		material[index] = byte(index)
	}
	token, err := Generate(bytes.NewReader(material))
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	if got := token.String(); got != base64.RawURLEncoding.EncodeToString(material) {
		t.Errorf("String() = %q", got)
	}
}

func TestGenerateTokenShortReader(t *testing.T) {
	_, err := Generate(bytes.NewReader(make([]byte, TokenSize-1)))
	if code, ok := CodeOf(err); !ok || code != CodeGenerationFailed {
		t.Fatalf("code = %q, present = %v, want %q", code, ok, CodeGenerationFailed)
	}
}

func TestTokenParseRoundTrip(t *testing.T) {
	original, err := Generate(bytes.NewReader(bytes.Repeat([]byte{0x42}, TokenSize)))
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	parsed, err := Parse(original.String())
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	if parsed != original {
		t.Errorf("Parse(String()) changed token")
	}
}

func TestTokenParseRejectsInvalidValues(t *testing.T) {
	valid := base64.RawURLEncoding.EncodeToString(make([]byte, TokenSize))
	tests := []string{"", "not-base64!", valid + "=", valid[:len(valid)-1], valid + "a", base64.RawURLEncoding.EncodeToString(make([]byte, TokenSize-1))}
	for index, input := range tests {
		t.Run(string(rune('a'+index)), func(t *testing.T) {
			_, err := Parse(input)
			if code, ok := CodeOf(err); !ok || code != CodeInvalidToken {
				t.Fatalf("code = %q, present = %v, want %q", code, ok, CodeInvalidToken)
			}
			if input != "" && strings.Contains(err.Error(), input) {
				t.Error("error echoed invalid token")
			}
		})
	}
}

func TestTokenVerify(t *testing.T) {
	token, err := Generate(bytes.NewReader(bytes.Repeat([]byte{0x11}, TokenSize)))
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	if !token.Verify(token.String()) {
		t.Error("Verify() rejected matching token")
	}
	other, _ := Generate(bytes.NewReader(bytes.Repeat([]byte{0x12}, TokenSize)))
	if token.Verify(other.String()) {
		t.Error("Verify() accepted different token")
	}
	if token.Verify("malformed") {
		t.Error("Verify() accepted malformed token")
	}
}

func TestTokenErrorDoesNotExposeMaterial(t *testing.T) {
	canary := "synthetic_token_marker_12345"
	_, err := Parse(canary)
	if err == nil {
		t.Fatal("Parse() succeeded")
	}
	if strings.Contains(err.Error(), canary) {
		t.Error("error exposed token material")
	}
	var tokenErr *Error
	if !errors.As(err, &tokenErr) {
		t.Fatalf("error type = %T, want *Error", err)
	}
}
