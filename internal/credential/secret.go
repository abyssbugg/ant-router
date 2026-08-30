package credential

// Secret is an opaque provider credential value.
type Secret struct {
	value string
}

func newSecret(value string) Secret { return Secret{value: value} }

// Reveal returns the credential for the narrow provider SDK boundary.
// Callers must not log, format, persist, or return the value in errors.
func (s Secret) Reveal() string { return s.value }

// String prevents ordinary formatting from exposing the credential.
func (s Secret) String() string { return "[REDACTED]" }

// GoString prevents Go-syntax formatting from exposing the credential.
func (s Secret) GoString() string { return "[REDACTED]" }
