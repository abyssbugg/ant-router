package credential

import "regexp"

const MaxNameBytes = 128

// SourceKind identifies a credential lookup mechanism.
type SourceKind string

const SourceEnv SourceKind = "env"

// Ref names a credential without containing its value.
type Ref struct {
	source SourceKind
	name   string
}

var environmentNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// NewRef constructs a validated credential reference.
func NewRef(source SourceKind, name string) (Ref, error) {
	ref := Ref{source: source, name: name}
	if source != SourceEnv {
		return Ref{}, newError(CodeInvalidReference, "credential.source", nil)
	}
	if len(name) == 0 || len(name) > MaxNameBytes || !environmentNamePattern.MatchString(name) {
		return Ref{}, newError(CodeInvalidReference, "credential.name", nil)
	}
	return ref, nil
}

// Source returns the reference source kind.
func (r Ref) Source() SourceKind { return r.source }

// Name returns the safe environment variable name.
func (r Ref) Name() string { return r.name }

// Valid reports whether the reference satisfies all invariants.
func (r Ref) Valid() bool {
	_, err := NewRef(r.source, r.name)
	return err == nil
}
