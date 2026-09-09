package canonical

import (
	"net/url"
	"unicode"
	"unicode/utf8"
)

// URLCitation is an immutable HTTPS citation attached to a text range.
type URLCitation struct {
	startIndex int
	endIndex   int
	title      string
	url        string
}

// NewURLCitation validates a ranged HTTPS citation.
func NewURLCitation(startIndex, endIndex int, title, reference string) (URLCitation, error) {
	value := URLCitation{startIndex: startIndex, endIndex: endIndex, title: title, url: reference}
	if !value.Valid() {
		code := CodeInvalidInvariant
		if !utf8.ValidString(title) || !utf8.ValidString(reference) {
			code = CodeInvalidUTF8
		} else if len(title) > MaxCitationTitleBytes || len(reference) > MaxHostedSearchURLBytes {
			code = CodeLimitExceeded
		}
		return URLCitation{}, contentError(code, "content.text.citation")
	}
	return value, nil
}

func (c URLCitation) StartIndex() int { return c.startIndex }
func (c URLCitation) EndIndex() int   { return c.endIndex }
func (c URLCitation) Title() string   { return c.title }
func (c URLCitation) URL() string     { return c.url }

// Valid reports whether a citation satisfies structural and URL invariants.
func (c URLCitation) Valid() bool {
	return c.startIndex >= 0 && c.endIndex > c.startIndex && validBoundedMetadata(c.title, MaxCitationTitleBytes, true) && validHTTPSReference(c.url)
}

func (c URLCitation) validForText(text string) bool {
	return c.Valid() && c.endIndex <= utf8.RuneCountInString(text)
}

func validHTTPSReference(reference string) bool {
	if reference == "" || !utf8.ValidString(reference) || len(reference) > MaxHostedSearchURLBytes {
		return false
	}
	for _, character := range reference {
		if unicode.IsControl(character) {
			return false
		}
	}
	parsed, err := url.Parse(reference)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func validBoundedMetadata(value string, limit int, emptyAllowed bool) bool {
	if (!emptyAllowed && value == "") || !utf8.ValidString(value) || len(value) > limit {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
