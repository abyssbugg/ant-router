package codexprofile

import (
	"bytes"
	"os"
	"testing"
)

func TestRenderMatchesGoldenProfile(t *testing.T) {
	want, err := os.ReadFile("testdata/profile.toml")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Render(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("rendered profile differs from golden fixture")
	}
}
