package canonical

import (
	"bytes"
	"strings"
	"testing"
)

func TestRemoteImage(t *testing.T) {
	image, err := NewRemoteImage("https://example.com/image.png?sig=abc", "image/png", ImageDetailHigh, 640, 480)
	if err != nil {
		t.Fatalf("NewRemoteImage() failed: %v", err)
	}
	url, ok := image.URL()
	if !ok || url != "https://example.com/image.png?sig=abc" || image.SourceType() != ImageSourceURL || image.MIME() != "image/png" || image.Detail() != ImageDetailHigh || image.Width() != 640 || image.Height() != 480 || !image.Valid() {
		t.Errorf("image = %#v URL=%q", image, url)
	}
	if data, ok := image.Bytes(); ok || data != nil {
		t.Error("remote image exposed inline bytes")
	}
}

func TestInlineImageDefensiveCopies(t *testing.T) {
	input := []byte{1, 2, 3, 4}
	image, err := NewInlineImage(input, "image/jpeg", ImageDetailAuto, 0, 0)
	if err != nil {
		t.Fatalf("NewInlineImage() failed: %v", err)
	}
	input[0] = 9
	first, ok := image.Bytes()
	if !ok {
		t.Fatal("inline image did not expose bytes")
	}
	first[1] = 9
	second, _ := image.Bytes()
	if !bytes.Equal(second, []byte{1, 2, 3, 4}) || image.SourceType() != ImageSourceInline {
		t.Errorf("inline image mutated: %v", second)
	}
	if url, ok := image.URL(); ok || url != "" {
		t.Error("inline image exposed URL")
	}
}

func TestImageRejectsInvalidSources(t *testing.T) {
	validData := []byte{1}
	tests := []struct {
		name string
		make func() error
		code ErrorCode
	}{
		{"http", func() error {
			_, err := NewRemoteImage("http://example.com/a.png", "image/png", ImageDetailAuto, 0, 0)
			return err
		}, CodeInvalidURL},
		{"userinfo", func() error {
			_, err := NewRemoteImage("https://user@example.com/a.png", "image/png", ImageDetailAuto, 0, 0)
			return err
		}, CodeInvalidURL},
		{"fragment", func() error {
			_, err := NewRemoteImage("https://example.com/a.png#x", "image/png", ImageDetailAuto, 0, 0)
			return err
		}, CodeInvalidURL},
		{"path", func() error { _, err := NewRemoteImage("/tmp/a.png", "image/png", ImageDetailAuto, 0, 0); return err }, CodeInvalidURL},
		{"long URL", func() error {
			_, err := NewRemoteImage("https://example.com/"+strings.Repeat("x", MaxImageURLBytes), "image/png", ImageDetailAuto, 0, 0)
			return err
		}, CodeLimitExceeded},
		{"empty inline", func() error { _, err := NewInlineImage(nil, "image/png", ImageDetailAuto, 0, 0); return err }, CodeInvalidInvariant},
		{"large inline", func() error {
			_, err := NewInlineImage(make([]byte, MaxInlineImageBytes+1), "image/png", ImageDetailAuto, 0, 0)
			return err
		}, CodeLimitExceeded},
		{"mime", func() error { _, err := NewInlineImage(validData, "image/svg+xml", ImageDetailAuto, 0, 0); return err }, CodeUnsupportedMediaType},
		{"detail", func() error {
			_, err := NewInlineImage(validData, "image/png", ImageDetail("medium"), 0, 0)
			return err
		}, CodeInvalidInvariant},
		{"one dimension", func() error { _, err := NewInlineImage(validData, "image/png", ImageDetailAuto, 10, 0); return err }, CodeInvalidInvariant},
		{"dimension zero", func() error { _, err := NewInlineImage(validData, "image/png", ImageDetailAuto, -1, -1); return err }, CodeInvalidInvariant},
		{"dimension limit", func() error {
			_, err := NewInlineImage(validData, "image/png", ImageDetailAuto, MaxImageDimension+1, MaxImageDimension+1)
			return err
		}, CodeLimitExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.make(); codeOfCanonical(err) != test.code {
				t.Errorf("code = %q, want %q; error: %v", codeOfCanonical(err), test.code, err)
			}
		})
	}
}

func TestImageErrorsDoNotEchoURLOrMIME(t *testing.T) {
	urlCanary := "https://synthetic-secret.example/path"
	mimeCanary := "image/synthetic-secret"
	_, err := NewRemoteImage(urlCanary, mimeCanary, ImageDetailAuto, 0, 0)
	if err == nil {
		t.Fatal("invalid MIME accepted")
	}
	if strings.Contains(err.Error(), urlCanary) || strings.Contains(err.Error(), mimeCanary) {
		t.Errorf("error leaked image metadata: %q", err)
	}
}
