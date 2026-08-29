package responses

import (
	"fmt"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestDecodeMixedTextAndImageContent(t *testing.T) {
	inline := dataURL("image/png", pngHeader(12, 7))
	body := fmt.Sprintf(`{
		"model":"mock/vision",
		"input":[{"role":"user","content":[
			{"type":"input_text","text":"before"},
			{"type":"input_image","image_url":%q,"detail":"high"},
			{"type":"input_text","text":"after"}
		]}]
	}`, inline)
	parsed, err := Decode(strings.NewReader(body), &sequenceIDs{})
	if err != nil {
		t.Fatalf("Decode() failed: %v", err)
	}
	items := parsed.Request.Input()
	if len(items) != 1 {
		t.Fatalf("input items = %#v", items)
	}
	message, ok := items[0].Message()
	if !ok {
		t.Fatalf("input item is not a message: %#v", items[0])
	}
	blocks := message.Content()
	if len(blocks) != 3 {
		t.Fatalf("content blocks = %d", len(blocks))
	}
	before, beforeOK := blocks[0].Text()
	image, imageOK := blocks[1].Image()
	after, afterOK := blocks[2].Text()
	data, dataOK := image.Bytes()
	if !beforeOK || before.Text() != "before" || !imageOK || !dataOK || image.MIME() != "image/png" || image.Detail() != canonical.ImageDetailHigh || image.Width() != 12 || image.Height() != 7 || !afterOK || after.Text() != "after" {
		t.Fatalf("mixed content was not preserved")
	}
	if !parsed.Request.Capabilities().Has(canonical.CapabilityImageInput) || !parsed.Request.Capabilities().Has(canonical.CapabilityText) {
		t.Fatalf("capabilities = %v", parsed.Request.Capabilities().Values())
	}
	data[0] ^= 0xff
	again, _ := image.Bytes()
	if data[0] == again[0] {
		t.Fatal("inline image bytes were not copied defensively")
	}
}

func TestDecodeRemoteImageUsesUnknownMIME(t *testing.T) {
	parsed, err := Decode(strings.NewReader(`{
		"model":"mock/vision",
		"input":[{"role":"user","content":[
			{"type":"input_image","image_url":"https://example.com/signed?private=value","detail":"auto"}
		]}]
	}`), &sequenceIDs{})
	if err != nil {
		t.Fatalf("Decode() failed: %v", err)
	}
	items := parsed.Request.Input()
	if len(items) != 1 {
		t.Fatalf("input items = %#v", items)
	}
	message, ok := items[0].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("input item is not the expected message: %#v", items[0])
	}
	image, ok := message.Content()[0].Image()
	url, urlOK := image.URL()
	if !ok || !urlOK || url != "https://example.com/signed?private=value" || image.MIME() != "" || image.Width() != 0 || image.Height() != 0 {
		t.Fatalf("remote image = %#v", image)
	}
}

func TestDecodeImageErrorsRetainContentIndex(t *testing.T) {
	body := `{
		"model":"mock/vision",
		"input":[{"role":"user","content":[
			{"type":"input_text","text":"before"},
			{"type":"input_image","file_id":"file-private"}
		]}]
	}`
	_, err := Decode(strings.NewReader(body), &sequenceIDs{})
	if CodeOf(err) != CodeUnsupportedField || PathOf(err) != "input[0].content[1].file_id" {
		t.Fatalf("Decode() error = %v", err)
	}
	if strings.Contains(err.Error(), "file-private") {
		t.Fatalf("error leaked file ID: %v", err)
	}
}
