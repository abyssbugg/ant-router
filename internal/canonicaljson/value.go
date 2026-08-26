package canonicaljson

import "bytes"

// Kind identifies the top-level JSON value type.
type Kind string

const (
	// KindInvalid is the zero-value kind and is never returned by Parse.
	KindInvalid Kind = ""
	// KindNull identifies JSON null.
	KindNull Kind = "null"
	// KindBool identifies a JSON boolean.
	KindBool Kind = "bool"
	// KindNumber identifies a JSON number with preserved lexical spelling.
	KindNumber Kind = "number"
	// KindString identifies a JSON string.
	KindString Kind = "string"
	// KindArray identifies a JSON array.
	KindArray Kind = "array"
	// KindObject identifies a JSON object.
	KindObject Kind = "object"
)

// Value is an immutable deterministically encoded JSON value.
type Value struct {
	canonical []byte
	kind      Kind
}

// Bytes returns a defensive copy of the canonical JSON bytes.
func (v Value) Bytes() []byte { return append([]byte(nil), v.canonical...) }

// Equal reports whether kind and canonical bytes are identical.
func (v Value) Equal(other Value) bool {
	return v.kind == other.kind && bytes.Equal(v.canonical, other.canonical)
}

// Kind returns the top-level JSON kind.
func (v Value) Kind() Kind { return v.kind }

// IsObject reports whether the top-level value is an object.
func (v Value) IsObject() bool { return v.kind == KindObject }

// IsNull reports whether the top-level value is null.
func (v Value) IsNull() bool { return v.kind == KindNull }

// MustParseFixture parses trusted fixture JSON and panics on invalid input.
// Production request paths must use Parse and handle its returned error.
func MustParseFixture(data []byte) Value {
	value, err := Parse(data, DefaultLimits())
	if err != nil {
		panic(err)
	}
	return value
}
