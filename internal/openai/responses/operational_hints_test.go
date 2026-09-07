package responses

import (
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestOperationalHintsAreValidatedAndNonsemantic(t *testing.T) {
	base, err := Decode(strings.NewReader(`{"model":"mock/text","input":"hi"}`), &sequenceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	withHints, err := Decode(strings.NewReader(`{"model":"mock/text","input":"hi","prompt_cache_key":"session-safe","service_tier":"auto"}`), &sequenceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := canonical.PortableDigest(base.Request)
	second, _ := canonical.PortableDigest(withHints.Request)
	if first != second {
		t.Fatal("operational hints changed canonical semantics")
	}
}

func TestOperationalHintsRejectInvalidValuesWithoutLeakage(t *testing.T) {
	secret := "operational-secret-marker"
	for _, tc := range []struct{ field, raw string }{
		{"prompt_cache_key", `null`}, {"prompt_cache_key", `""`},
		{"service_tier", `123`}, {"service_tier", `"line\nbreak"`},
		{"prompt_cache_key", `"` + strings.Repeat("x", maxOperationalHintBytes+1) + secret + `"`},
	} {
		body := `{"model":"mock/text","input":"hi","` + tc.field + `":` + tc.raw + `}`
		_, err := Decode(strings.NewReader(body), &sequenceIDs{})
		if err == nil {
			t.Fatalf("accepted invalid %s", tc.field)
		}
		if PathOf(err) != tc.field {
			t.Fatalf("path=%q want=%q", PathOf(err), tc.field)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatal("operational hint leaked")
		}
	}
}
