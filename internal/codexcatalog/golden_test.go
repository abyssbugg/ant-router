package codexcatalog

import (
	"bytes"
	"os"
	"testing"
)

func TestGenerateMatchesGoldenCatalog(t *testing.T) {
	want, err := os.ReadFile("testdata/models.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Generate([]Projection{projection("openai/gpt-test", 10, "text", "image_input", "reasoning_summary")})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("generated catalog differs from golden fixture")
	}
}
