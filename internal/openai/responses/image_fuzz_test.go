package responses

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzDecodeImage(f *testing.F) {
	for _, seed := range []struct {
		source string
		detail string
	}{
		{"https://example.com/image", "auto"},
		{dataURL("image/png", pngHeader(16, 8)), "high"},
		{"data:image/png;base64,%%%PRIVATE_IMAGE_MARKER%%%", "low"},
		{"file:///private/image.png", "auto"},
	} {
		f.Add(seed.source, seed.detail)
	}
	f.Fuzz(func(t *testing.T, source, detail string) {
		if len(source) > maxEncodedInlineImage+64 || len(detail) > 64 {
			t.Skip()
		}
		fields := map[string]json.RawMessage{
			"type":      json.RawMessage(`"input_image"`),
			"image_url": mustMarshalFuzz(t, source),
			"detail":    mustMarshalFuzz(t, detail),
		}
		block, err := decodeImageBlock(fields, "input[0].content[0]")
		if err != nil {
			if strings.Contains(err.Error(), "PRIVATE_IMAGE_MARKER") || (len(source) >= 32 && strings.Contains(err.Error(), source)) {
				t.Fatalf("error leaked image input: %v", err)
			}
			return
		}
		image, ok := block.Image()
		if !block.Valid() || !ok || !image.Valid() {
			t.Fatalf("successful decode produced invalid canonical image")
		}
	})
}

func FuzzImageDimensions(f *testing.F) {
	f.Add("image/png", pngHeader(16, 8))
	f.Add("image/jpeg", jpegHeader(16, 8))
	f.Add("image/gif", gifHeader(16, 8))
	f.Add("image/webp", webpVP8XHeader(16, 8))
	f.Fuzz(func(t *testing.T, mime string, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		width, height, err := imageDimensions(mime, data)
		if err == nil && (width <= 0 || height <= 0) {
			t.Fatalf("successful dimension parse returned %dx%d", width, height)
		}
	})
}

func mustMarshalFuzz(t *testing.T, value string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
