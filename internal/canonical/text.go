package canonical

import (
	"errors"
	"unicode/utf8"
)

// TextContent is an immutable bounded UTF-8 text block with optional URL citations.
type TextContent struct {
	text      string
	citations []URLCitation
}

// NewTextContent validates and returns text content without citations.
func NewTextContent(text string) (TextContent, error) {
	return NewTextContentWithCitations(text, nil)
}

// NewTextContentWithCitations validates and defensively copies text citations.
func NewTextContentWithCitations(text string, citations []URLCitation) (TextContent, error) {
	value := TextContent{text: text, citations: append([]URLCitation(nil), citations...)}
	if !value.Valid() {
		code := CodeInvalidInvariant
		if !utf8.ValidString(text) {
			code = CodeInvalidUTF8
		} else if len(text) > MaxTextBytes || len(citations) > MaxTextCitations {
			code = CodeLimitExceeded
		}
		return TextContent{}, contentError(code, "content.text")
	}
	return value, nil
}

// Text returns the immutable text value.
func (c TextContent) Text() string { return c.text }

// Citations returns a defensive copy of URL citations.
func (c TextContent) Citations() []URLCitation { return append([]URLCitation(nil), c.citations...) }

// Valid reports whether the text content satisfies canonical invariants.
func (c TextContent) Valid() bool {
	if c.text == "" || !utf8.ValidString(c.text) || len(c.text) > MaxTextBytes || len(c.citations) > MaxTextCitations {
		return false
	}
	seen := make(map[URLCitation]struct{}, len(c.citations))
	previousStart := -1
	for _, citation := range c.citations {
		if !citation.validForText(c.text) || citation.StartIndex() < previousStart {
			return false
		}
		if _, duplicate := seen[citation]; duplicate {
			return false
		}
		seen[citation] = struct{}{}
		previousStart = citation.StartIndex()
	}
	return true
}

func contentError(code ErrorCode, path string) error {
	return &Error{Code: code, Path: path, Err: errors.New("content is invalid")}
}
