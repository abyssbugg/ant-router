package responses

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestDecodeImageBlockRemoteHTTPS(t *testing.T) {
	block, err := decodeImageBlock(imageFields(t, map[string]any{
		"type":      "input_image",
		"image_url": "https://example.com/signed-image?token=private",
		"detail":    "high",
	}), "input[0].content[1]")
	if err != nil {
		t.Fatalf("decodeImageBlock() failed: %v", err)
	}
	image, ok := block.Image()
	url, urlOK := image.URL()
	if !ok || !urlOK || url != "https://example.com/signed-image?token=private" || image.MIME() != "" || image.Detail() != canonical.ImageDetailHigh || image.Width() != 0 || image.Height() != 0 {
		t.Fatalf("remote image = %#v", image)
	}
}

func TestDecodeImageBlockDataFormats(t *testing.T) {
	cases := []struct {
		name, mime string
		data       []byte
		width      int
		height     int
	}{
		{"png", "image/png", pngHeader(640, 480), 640, 480},
		{"jpeg", "image/jpeg", jpegHeader(320, 240), 320, 240},
		{"gif", "image/gif", gifHeader(200, 100), 200, 100},
		{"webp", "image/webp", webpVP8XHeader(1024, 768), 1024, 768},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block, err := decodeImageBlock(imageFields(t, map[string]any{
				"type":      "input_image",
				"image_url": dataURL(tc.mime, tc.data),
				"detail":    "low",
			}), "input[0].content[0]")
			if err != nil {
				t.Fatalf("decodeImageBlock() failed: %v", err)
			}
			image, ok := block.Image()
			decoded, bytesOK := image.Bytes()
			if !ok || !bytesOK || string(decoded) != string(tc.data) || image.MIME() != tc.mime || image.Detail() != canonical.ImageDetailLow || image.Width() != tc.width || image.Height() != tc.height {
				t.Fatalf("inline image = %#v", image)
			}
		})
	}
}

func TestDecodeImageBlockRejectsInvalidSources(t *testing.T) {
	oversized := strings.Repeat("A", base64.StdEncoding.EncodedLen(canonical.MaxInlineImageBytes)+1)
	cases := []struct {
		name   string
		fields map[string]any
		code   ErrorCode
		path   string
	}{
		{"file ID", map[string]any{"type": "input_image", "file_id": "file-private"}, CodeUnsupportedField, "input[0].content[0].file_id"},
		{"HTTP URL", map[string]any{"type": "input_image", "image_url": "http://private.example/image.png"}, CodeInvalidField, "input[0].content[0].image_url"},
		{"unsupported MIME", map[string]any{"type": "input_image", "image_url": "data:image/svg+xml;base64,PHN2Zz4="}, CodeUnsupportedField, "input[0].content[0].image_url"},
		{"invalid base64", map[string]any{"type": "input_image", "image_url": "data:image/png;base64,%%%PRIVATE%%%"}, CodeInvalidField, "input[0].content[0].image_url"},
		{"signature mismatch", map[string]any{"type": "input_image", "image_url": dataURL("image/png", gifHeader(2, 2))}, CodeInvalidField, "input[0].content[0].image_url"},
		{"truncated header", map[string]any{"type": "input_image", "image_url": dataURL("image/png", []byte("\x89PNG\r\n\x1a\n"))}, CodeInvalidField, "input[0].content[0].image_url"},
		{"zero dimension", map[string]any{"type": "input_image", "image_url": dataURL("image/png", pngHeader(0, 2))}, CodeInvalidField, "input[0].content[0].image_url"},
		{"dimension limit", map[string]any{"type": "input_image", "image_url": dataURL("image/png", pngHeader(canonical.MaxImageDimension+1, 1))}, CodeLimitExceeded, "input[0].content[0].image_url"},
		{"PNG uint32 boundary", map[string]any{"type": "input_image", "image_url": dataURL("image/png", pngHeaderRaw(^uint32(0), 1))}, CodeLimitExceeded, "input[0].content[0].image_url"},
		{"VP8X 24-bit boundary", map[string]any{"type": "input_image", "image_url": dataURL("image/webp", webpVP8XHeader(1<<24, 1))}, CodeLimitExceeded, "input[0].content[0].image_url"},
		{"pixel limit", map[string]any{"type": "input_image", "image_url": dataURL("image/png", pngHeader(20_000, 20_000))}, CodeLimitExceeded, "input[0].content[0].image_url"},
		{"encoded limit", map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + oversized}, CodeLimitExceeded, "input[0].content[0].image_url"},
		{"invalid detail", map[string]any{"type": "input_image", "image_url": "https://example.com/image", "detail": "private-detail"}, CodeInvalidField, "input[0].content[0].detail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeImageBlock(imageFields(t, tc.fields), "input[0].content[0]")
			if CodeOf(err) != tc.code || PathOf(err) != tc.path {
				t.Fatalf("error=%v code/path=%q/%q want=%q/%q", err, CodeOf(err), PathOf(err), tc.code, tc.path)
			}
			for _, secret := range []string{"private", "PRIVATE", "file-private"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaked image input: %v", err)
				}
			}
		})
	}
}

func imageFields(t *testing.T, value map[string]any) map[string]json.RawMessage {
	t.Helper()
	fields := make(map[string]json.RawMessage, len(value))
	for key, field := range value {
		raw, err := json.Marshal(field)
		if err != nil {
			t.Fatal(err)
		}
		fields[key] = raw
	}
	return fields
}

func dataURL(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func pngHeader(width, height int) []byte {
	return pngHeaderRaw(uint32(width), uint32(height))
}

func pngHeaderRaw(width, height uint32) []byte {
	data := make([]byte, 24)
	copy(data, []byte("\x89PNG\r\n\x1a\n"))
	binary.BigEndian.PutUint32(data[8:12], 13)
	copy(data[12:16], "IHDR")
	binary.BigEndian.PutUint32(data[16:20], width)
	binary.BigEndian.PutUint32(data[20:24], height)
	return data
}

func jpegHeader(width, height int) []byte {
	data := make([]byte, 21)
	copy(data, []byte{0xff, 0xd8, 0xff, 0xc0, 0x00, 0x11, 0x08, byte(height >> 8), byte(height), byte(width >> 8), byte(width)})
	return data
}

func gifHeader(width, height int) []byte {
	data := make([]byte, 10)
	copy(data, "GIF89a")
	binary.LittleEndian.PutUint16(data[6:8], uint16(width))
	binary.LittleEndian.PutUint16(data[8:10], uint16(height))
	return data
}

func webpVP8XHeader(width, height int) []byte {
	data := make([]byte, 30)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:12], "WEBP")
	copy(data[12:16], "VP8X")
	binary.LittleEndian.PutUint32(data[16:20], 10)
	data[24], data[25], data[26] = byte(width-1), byte((width-1)>>8), byte((width-1)>>16)
	data[27], data[28], data[29] = byte(height-1), byte((height-1)>>8), byte((height-1)>>16)
	return data
}
