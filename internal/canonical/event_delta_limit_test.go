package canonical

import (
	"strings"
	"testing"
)

func TestDeltaEventsEncodeWithinWireLimit(t *testing.T) {
	itemID := testItemID(t, 110)
	responseID := testResponseID(t, 110)
	// A delta that exactly fits MaxOrdinaryEventBytes must still encode: the
	// encoded envelope adds IDs and field names on top of the raw delta bytes.
	filler := strings.Repeat("a", MaxOrdinaryEventBytes-256)
	event, err := NewTextDeltaEvent(testEventID(t, 110), responseID, 1, itemID, 0, filler)
	if err != nil {
		t.Fatalf("text delta constructor: %v", err)
	}
	if _, err := EncodeEvent(event); err != nil {
		t.Fatalf("encoded text delta exceeded wire limit: %v", err)
	}
	toolEvent, err := NewToolArgumentsDeltaEvent(testEventID(t, 111), responseID, 1, itemID, filler)
	if err != nil {
		t.Fatalf("tool arguments delta constructor: %v", err)
	}
	if _, err := EncodeEvent(toolEvent); err != nil {
		t.Fatalf("encoded tool arguments delta exceeded wire limit: %v", err)
	}
}

func TestEscapedDeltaRejectsAtConstructor(t *testing.T) {
	itemID := testItemID(t, 112)
	responseID := testResponseID(t, 112)
	// Quotes double in encoded form; the constructor must reject deltas whose
	// encoded size would exceed the wire limit so EncodeEvent cannot fail.
	filler := strings.Repeat("\"", MaxOrdinaryEventBytes-256)
	_, err := NewTextDeltaEvent(testEventID(t, 112), responseID, 1, itemID, 0, filler)
	if codeOfCanonical(err) != CodeLimitExceeded {
		t.Fatalf("escaped oversized text delta error = %v", err)
	}
	_, err = NewJSONDeltaEvent(testEventID(t, 113), responseID, 1, itemID, 0, filler)
	if codeOfCanonical(err) != CodeLimitExceeded {
		t.Fatalf("escaped oversized json delta error = %v", err)
	}
	_, err = NewToolArgumentsDeltaEvent(testEventID(t, 114), responseID, 1, itemID, filler)
	if codeOfCanonical(err) != CodeLimitExceeded {
		t.Fatalf("escaped oversized tool delta error = %v", err)
	}
}
