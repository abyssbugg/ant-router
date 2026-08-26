package canonical

import "github.com/abyssbugg/ant-router/internal/canonicaljson"

// JSONContent wraps an immutable canonical JSON value.
type JSONContent struct{ value canonicaljson.Value }

// NewJSONContent validates and wraps a canonical JSON value.
func NewJSONContent(value canonicaljson.Value) (JSONContent, error) {
	if value.Kind() == canonicaljson.KindInvalid {
		return JSONContent{}, contentError(CodeInvalidJSON, "content.json")
	}
	if len(value.Bytes()) > MaxJSONBytes {
		return JSONContent{}, contentError(CodeLimitExceeded, "content.json")
	}
	return JSONContent{value: value}, nil
}

// Value returns the immutable canonical JSON value.
func (c JSONContent) Value() canonicaljson.Value { return c.value }

// Valid reports whether the JSON content satisfies canonical invariants.
func (c JSONContent) Valid() bool {
	return c.value.Kind() != canonicaljson.KindInvalid && len(c.value.Bytes()) <= MaxJSONBytes
}
