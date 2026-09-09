package canonical

import (
	"strings"
	"testing"
)

func TestURLCitationAndTextImmutability(t *testing.T) {
	citation, err := NewURLCitation(0, 4, "Example", "https://example.com/page?q=1#section")
	if err != nil || !citation.Valid() || citation.StartIndex() != 0 || citation.EndIndex() != 4 {
		t.Fatalf("citation = %#v, err = %v", citation, err)
	}
	text, err := NewTextContentWithCitations("test output", []URLCitation{citation})
	if err != nil || !text.Valid() || len(text.Citations()) != 1 {
		t.Fatalf("text = %#v, err = %v", text, err)
	}
	citations := text.Citations()
	citations[0] = URLCitation{}
	if !text.Citations()[0].Valid() {
		t.Fatal("citation slice was not immutable")
	}
	plain, err := NewTextContent("plain")
	if err != nil || len(plain.Citations()) != 0 {
		t.Fatalf("plain text citations = %#v, err = %v", plain.Citations(), err)
	}
}

func TestURLCitationRejectsInvalid(t *testing.T) {
	tests := []func() error{
		func() error { _, err := NewURLCitation(-1, 2, "title", "https://example.com"); return err },
		func() error { _, err := NewURLCitation(2, 2, "title", "https://example.com"); return err },
		func() error {
			_, err := NewURLCitation(0, 2, strings.Repeat("t", MaxCitationTitleBytes+1), "https://example.com")
			return err
		},
		func() error { _, err := NewURLCitation(0, 2, "bad\x00title", "https://example.com"); return err },
		func() error { _, err := NewURLCitation(0, 2, "title", "http://example.com"); return err },
		func() error { _, err := NewURLCitation(0, 2, "title", "https://user@example.com"); return err },
		func() error { _, err := NewURLCitation(0, 2, "title", "https://example.com/\x00"); return err },
	}
	for index, makeError := range tests {
		if err := makeError(); err == nil {
			t.Fatalf("invalid citation %d accepted", index)
		}
	}
}

func TestTextCitationsValidateRangesAndOrdering(t *testing.T) {
	first, _ := NewURLCitation(0, 4, "First", "https://example.com/first")
	second, _ := NewURLCitation(5, 11, "Second", "https://example.com/second")
	if _, err := NewTextContentWithCitations("hello world", []URLCitation{first, second}); err != nil {
		t.Fatalf("ordered citations rejected: %v", err)
	}
	outOfRange, _ := NewURLCitation(0, 12, "Range", "https://example.com/range")
	if _, err := NewTextContentWithCitations("hello world", []URLCitation{outOfRange}); err == nil {
		t.Fatal("out-of-range citation accepted")
	}
	if _, err := NewTextContentWithCitations("hello world", []URLCitation{second, first}); err == nil {
		t.Fatal("out-of-order citations accepted")
	}
	if _, err := NewTextContentWithCitations("hello world", []URLCitation{first, first}); err == nil {
		t.Fatal("duplicate citations accepted")
	}
}

func TestURLCitationAddedEvent(t *testing.T) {
	citation, _ := NewURLCitation(0, 4, "Example", "https://example.com")
	event, err := NewURLCitationAddedEvent(testEventID(t, 116), testResponseID(t, 116), 0, testItemID(t, 116), 1, citation)
	if err != nil || !event.Valid() || event.Type() != EventType("url_citation_added") {
		t.Fatalf("citation event = %#v, err = %v", event, err)
	}
	payload, ok := event.URLCitationAdded()
	if !ok || payload.ContentIndex() != 1 || payload.Citation().URL() != "https://example.com" {
		t.Fatalf("citation payload = %#v, %v", payload, ok)
	}
	if _, err := NewURLCitationAddedEvent(testEventID(t, 117), testResponseID(t, 117), 0, testItemID(t, 117), MaxContentBlocks, citation); err == nil {
		t.Fatal("invalid content index accepted")
	}
}

func TestCitationErrorsRedactValues(t *testing.T) {
	const secret = "citation-secret-97410de74c354fb7"
	_, err := NewURLCitation(0, 2, secret+"\x00", "https://example.com/"+secret)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("citation value leaked in error: %v", err)
	}
}

func TestTextCitationCountAndUnicodeRanges(t *testing.T) {
	unicodeCitation, _ := NewURLCitation(1, 2, "Emoji", "https://example.com/emoji")
	if _, err := NewTextContentWithCitations("a🙂b", []URLCitation{unicodeCitation}); err != nil {
		t.Fatalf("Unicode code-point citation rejected: %v", err)
	}
	tooFar, _ := NewURLCitation(1, 4, "Range", "https://example.com/range")
	if _, err := NewTextContentWithCitations("a🙂b", []URLCitation{tooFar}); err == nil {
		t.Fatal("citation beyond Unicode code-point length accepted")
	}
	citations := make([]URLCitation, MaxTextCitations+1)
	for index := range citations {
		citations[index], _ = NewURLCitation(0, 1, string(rune('a'+index%26)), "https://example.com/source")
	}
	if _, err := NewTextContentWithCitations("x", citations); codeOfCanonical(err) != CodeLimitExceeded {
		t.Fatalf("citation count error = %v", err)
	}
}

func FuzzURLCitation(f *testing.F) {
	f.Add(0, 4, "Example", "https://example.com", "test")
	f.Add(-1, 2, "Bad", "http://example.com", "text")
	f.Fuzz(func(t *testing.T, start, end int, title, reference, text string) {
		if len(title) > MaxCitationTitleBytes+1 || len(reference) > MaxHostedSearchURLBytes+1 || len(text) > MaxTextBytes+1 {
			return
		}
		citation, err := NewURLCitation(start, end, title, reference)
		if err != nil {
			return
		}
		if !citation.Valid() {
			t.Fatal("constructor returned invalid citation")
		}
		content, contentErr := NewTextContentWithCitations(text, []URLCitation{citation})
		if contentErr == nil && !content.Valid() {
			t.Fatal("constructor returned invalid cited text")
		}
	})
}
