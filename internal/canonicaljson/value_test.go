package canonicaljson

import (
	"bytes"
	"testing"
)

func TestParseCanonicalizesObjectsAndWhitespace(t *testing.T) {
	value, err := Parse([]byte(" { \"z\" : [3, 2, 1], \"a\" : {\"y\":true,\"x\":null} } \n"), DefaultLimits())
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	want := `{"a":{"x":null,"y":true},"z":[3,2,1]}`
	if string(value.Bytes()) != want {
		t.Errorf("Bytes() = %q, want %q", value.Bytes(), want)
	}
	if !value.IsObject() || value.IsNull() || value.Kind() != KindObject {
		t.Errorf("kind predicates are inconsistent: %q", value.Kind())
	}
}

func TestParsePreservesNumberLexemesAndArrayOrder(t *testing.T) {
	input := []byte(`[1,1.0,1e0,-2.50,0.001]`)
	value, err := Parse(input, DefaultLimits())
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	if !bytes.Equal(value.Bytes(), input) || value.Kind() != KindArray {
		t.Errorf("Bytes() = %q", value.Bytes())
	}
}

func TestValueDefensiveCopiesAndEquality(t *testing.T) {
	input := []byte(`{"a":1}`)
	value, err := Parse(input, DefaultLimits())
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	input[2] = 'z'
	first := value.Bytes()
	first[2] = 'z'
	second := value.Bytes()
	if string(second) != `{"a":1}` {
		t.Errorf("value was mutated: %q", second)
	}
	equal, _ := Parse([]byte(` {"a" : 1 } `), DefaultLimits())
	different, _ := Parse([]byte(`{"a":1.0}`), DefaultLimits())
	if !value.Equal(equal) || value.Equal(different) {
		t.Error("Equal() ignored canonical bytes")
	}
}

func TestKinds(t *testing.T) {
	tests := []struct {
		input string
		kind  Kind
	}{
		{"null", KindNull},
		{"true", KindBool},
		{"1", KindNumber},
		{`"x"`, KindString},
		{"[]", KindArray},
		{"{}", KindObject},
	}
	for _, test := range tests {
		value, err := Parse([]byte(test.input), DefaultLimits())
		if err != nil || value.Kind() != test.kind {
			t.Errorf("Parse(%q) = kind %q, %v", test.input, value.Kind(), err)
		}
		if value.IsNull() != (test.kind == KindNull) {
			t.Errorf("IsNull(%q) inconsistent", test.input)
		}
	}
}

func TestUnicodeAndEscaping(t *testing.T) {
	value, err := Parse([]byte(`{"emoji":"😀","escaped":"quote\\\" slash\\\\"}`), DefaultLimits())
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	if string(value.Bytes()) != `{"emoji":"😀","escaped":"quote\\\" slash\\\\"}` {
		t.Errorf("canonical Unicode/escaping = %q", value.Bytes())
	}
}

func TestMustParseFixture(t *testing.T) {
	if got := MustParseFixture([]byte(`{"a":1}`)); string(got.Bytes()) != `{"a":1}` {
		t.Errorf("MustParseFixture() = %q", got.Bytes())
	}
	defer func() {
		if recover() == nil {
			t.Error("MustParseFixture() did not panic for invalid JSON")
		}
	}()
	_ = MustParseFixture([]byte(`{`))
}
