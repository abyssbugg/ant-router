package codexprofile

import (
	"bytes"
	"os"
	"runtime"
	"testing"
)

func TestRenderMatchesGoldenProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("golden profile fixture uses stable Unix paths")
	}
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
