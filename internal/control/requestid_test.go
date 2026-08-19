package control

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestGenerateRequestID(t *testing.T) {
	material := bytes.Repeat([]byte{0x42}, RequestIDBytes)
	id, err := GenerateRequestID(bytes.NewReader(material))
	if err != nil {
		t.Fatalf("GenerateRequestID() failed: %v", err)
	}
	if !ValidRequestID(id) || strings.ContainsAny(id, "\r\n") {
		t.Errorf("generated invalid request ID %q", id)
	}
}

func TestGenerateRequestIDShortReader(t *testing.T) {
	if _, err := GenerateRequestID(bytes.NewReader(make([]byte, RequestIDBytes-1))); err == nil {
		t.Fatal("GenerateRequestID() accepted short randomness")
	}
}

func TestValidRequestID(t *testing.T) {
	valid := []string{"a", "request-123", "abc.DEF_123", strings.Repeat("a", MaxRequestIDLength)}
	for _, value := range valid {
		if !ValidRequestID(value) {
			t.Errorf("ValidRequestID(%q) = false", value)
		}
	}
	invalid := []string{"", strings.Repeat("a", MaxRequestIDLength+1), "space value", "line\nbreak", "slash/value", "ümlaut"}
	for _, value := range invalid {
		if ValidRequestID(value) {
			t.Errorf("ValidRequestID(%q) = true", value)
		}
	}
}

func TestRequestIDContext(t *testing.T) {
	ctx := WithRequestID(context.Background(), "request-123")
	if got := RequestIDFromContext(ctx); got != "request-123" {
		t.Errorf("RequestIDFromContext() = %q", got)
	}
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("empty context ID = %q", got)
	}
}
