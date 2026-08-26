package canonical

import (
	"bytes"
	"strings"
	"testing"
)

func TestContinuationDefensiveCopy(t *testing.T) {
	data := []byte{1, 2, 3}
	continuation, err := NewContinuation("openai", "gpt-model", data)
	if err != nil || !continuation.Valid() {
		t.Fatalf("NewContinuation() = %#v, %v", continuation, err)
	}
	data[0] = 9
	first := continuation.Bytes()
	first[1] = 9
	if !bytes.Equal(continuation.Bytes(), []byte{1, 2, 3}) || continuation.Provider() != "openai" || continuation.Model() != "gpt-model" {
		t.Error("continuation was mutated")
	}
}

func TestContinuationRejectsInvalid(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		data     []byte
		code     ErrorCode
	}{
		{"", "model", []byte{1}, CodeContinuationAffinityRequired},
		{"Bad Provider", "model", []byte{1}, CodeContinuationAffinityRequired},
		{"openai", "", []byte{1}, CodeContinuationAffinityRequired},
		{"openai", string([]byte{0xff}), []byte{1}, CodeInvalidUTF8},
		{"openai", strings.Repeat("m", MaxModelNameBytes+1), []byte{1}, CodeLimitExceeded},
		{"openai", "model", nil, CodeInvalidInvariant},
		{"openai", "model", make([]byte, MaxContinuationBytes+1), CodeLimitExceeded},
	}
	for _, test := range tests {
		if _, err := NewContinuation(test.provider, test.model, test.data); codeOfCanonical(err) != test.code {
			t.Errorf("provider=%q code=%q want=%q", test.provider, codeOfCanonical(err), test.code)
		}
	}
}
