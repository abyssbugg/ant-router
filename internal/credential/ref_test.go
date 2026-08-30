package credential

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNewRefValidatesEnvironmentReferences(t *testing.T) {
	ref, err := NewRef(SourceEnv, "OPENAI_API_KEY")
	if err != nil {
		t.Fatalf("NewRef() error = %v", err)
	}
	if ref.Source() != SourceEnv || ref.Name() != "OPENAI_API_KEY" || !ref.Valid() {
		t.Fatalf("reference = %#v", ref)
	}

	for _, test := range []struct {
		name   string
		source SourceKind
		value  string
	}{
		{"unsupported source", "file", "OPENAI_API_KEY"},
		{"missing name", SourceEnv, ""},
		{"lowercase name", SourceEnv, "openai_key"},
		{"hyphenated name", SourceEnv, "OPENAI-KEY"},
		{"oversized name", SourceEnv, strings.Repeat("A", MaxNameBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewRef(test.source, test.value); err == nil {
				t.Fatal("NewRef() accepted invalid reference")
			}
		})
	}
}

func TestSecretFormattingIsAlwaysRedacted(t *testing.T) {
	secret := newSecret("never-print-this-provider-key")
	for _, formatted := range []string{
		fmt.Sprint(secret),
		fmt.Sprintf("%s", secret),
		fmt.Sprintf("%q", secret),
		fmt.Sprintf("%v", secret),
		fmt.Sprintf("%+v", secret),
		fmt.Sprintf("%#v", secret),
	} {
		if strings.Contains(formatted, "never-print") || !strings.Contains(formatted, "REDACTED") {
			t.Fatalf("formatted secret = %q", formatted)
		}
	}
	if secret.Reveal() != "never-print-this-provider-key" {
		t.Fatal("Reveal() did not return the credential value")
	}
}

func TestCredentialErrorIsNilSafeAndRedacted(t *testing.T) {
	var typedNil *Error
	if typedNil.Error() != "" || typedNil.Unwrap() != nil || CodeOf(typedNil) != "" {
		t.Fatal("typed nil credential error is unsafe")
	}
	cause := errors.New("never-print-this-provider-key")
	err := newError(CodeMissing, "credential", cause)
	if strings.Contains(err.Error(), cause.Error()) || !errors.Is(err, cause) || CodeOf(err) != CodeMissing {
		t.Fatalf("credential error = %v", err)
	}
}
