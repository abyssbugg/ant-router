package canonical

import (
	"errors"
	"unicode/utf8"
)

// TextContent is an immutable bounded UTF-8 text block.
type TextContent struct{ text string }

// NewTextContent validates and returns text content.
func NewTextContent(text string) (TextContent, error) {
	if text == "" {
		return TextContent{}, contentError(CodeInvalidInvariant, "content.text")
	}
	if !utf8.ValidString(text) {
		return TextContent{}, contentError(CodeInvalidUTF8, "content.text")
	}
	if len(text) > MaxTextBytes {
		return TextContent{}, contentError(CodeLimitExceeded, "content.text")
	}
	return TextContent{text: text}, nil
}

// Text returns the immutable text value.
func (c TextContent) Text() string { return c.text }

// Valid reports whether the text content satisfies canonical invariants.
func (c TextContent) Valid() bool {
	return c.text != "" && utf8.ValidString(c.text) && len(c.text) <= MaxTextBytes
}

func contentError(code ErrorCode, path string) error {
	return &Error{Code: code, Path: path, Err: errors.New("content is invalid")}
}
