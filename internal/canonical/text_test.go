package canonical

import (
	"strings"
	"testing"
)

func TestTextContent(t *testing.T) {
	content, err := NewTextContent("hello")
	if err != nil || content.Text() != "hello" || !content.Valid() {
		t.Fatalf("NewTextContent() = %q, %v", content.Text(), err)
	}
	max := strings.Repeat("x", MaxTextBytes)
	if _, err := NewTextContent(max); err != nil {
		t.Fatalf("maximum text rejected: %v", err)
	}
}

func TestTextContentRejectsInvalid(t *testing.T) {
	invalidUTF8 := string([]byte{0xff})
	tests := []struct {
		name string
		text string
		code ErrorCode
	}{
		{"empty", "", CodeInvalidInvariant},
		{"utf8", invalidUTF8, CodeInvalidUTF8},
		{"limit", strings.Repeat("x", MaxTextBytes+1), CodeLimitExceeded},
	}
	for _, test := range tests {
		if _, err := NewTextContent(test.text); codeOfCanonical(err) != test.code {
			t.Errorf("%s code = %q, want %q", test.name, codeOfCanonical(err), test.code)
		}
	}
}
