package canonical

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func TestContentBlockVariants(t *testing.T) {
	text, _ := NewTextContent("hello")
	textBlock, err := NewTextBlock(text)
	if err != nil || textBlock.Type() != ContentType("text") || !textBlock.Valid() {
		t.Fatalf("text block = %#v, %v", textBlock, err)
	}
	if got, ok := textBlock.Text(); !ok || got.Text() != "hello" {
		t.Errorf("Text() = %#v, %v", got, ok)
	}

	image, _ := NewInlineImage([]byte{1}, "image/png", ImageDetailAuto, 0, 0)
	imageBlock, _ := NewImageBlock(image)
	if got, ok := imageBlock.Image(); !ok || got.MIME() != "image/png" || imageBlock.Type() != ContentType("image") {
		t.Errorf("Image() = %#v, %v", got, ok)
	}

	value, _ := canonicaljson.Parse([]byte(`{"a":1}`), canonicaljson.DefaultLimits())
	jsonContent, _ := NewJSONContent(value)
	jsonBlock, _ := NewJSONBlock(jsonContent)
	if got, ok := jsonBlock.JSON(); !ok || !got.Value().Equal(value) || jsonBlock.Type() != ContentType("json") {
		t.Errorf("JSON() = %#v, %v", got, ok)
	}
}

func TestContentBlockRejectsZeroValues(t *testing.T) {
	if (ContentBlock{}).Valid() {
		t.Error("zero ContentBlock is valid")
	}
	if _, err := NewTextBlock(TextContent{}); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Errorf("zero text block code = %q", codeOfCanonical(err))
	}
	if _, err := NewImageBlock(ImageContent{}); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Errorf("zero image block code = %q", codeOfCanonical(err))
	}
	if _, err := NewJSONBlock(JSONContent{}); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Errorf("zero JSON block code = %q", codeOfCanonical(err))
	}
}
