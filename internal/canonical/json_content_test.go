package canonical

import (
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func TestJSONContent(t *testing.T) {
	value, err := canonicaljson.Parse([]byte(`{"b":2,"a":1}`), canonicaljson.DefaultLimits())
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	content, err := NewJSONContent(value)
	if err != nil || !content.Valid() || string(content.Value().Bytes()) != `{"a":1,"b":2}` {
		t.Fatalf("NewJSONContent() = %q, %v", content.Value().Bytes(), err)
	}
}

func TestJSONContentRejectsInvalidOrOversized(t *testing.T) {
	if _, err := NewJSONContent(canonicaljson.Value{}); codeOfCanonical(err) != CodeInvalidJSON {
		t.Errorf("zero JSON code = %q", codeOfCanonical(err))
	}
	data := []byte(`"` + strings.Repeat("x", MaxJSONBytes) + `"`)
	value, err := canonicaljson.Parse(data, canonicaljson.Limits{MaximumBytes: len(data), MaximumDepth: MaxJSONDepth})
	if err != nil {
		t.Fatalf("large Parse() failed: %v", err)
	}
	if _, err := NewJSONContent(value); codeOfCanonical(err) != CodeLimitExceeded {
		t.Errorf("oversized JSON code = %q", codeOfCanonical(err))
	}
}
