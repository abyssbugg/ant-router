package canonical

// ContentBlock is an immutable discriminated canonical content value.
type ContentBlock struct {
	kind  ContentType
	text  TextContent
	image ImageContent
	json  JSONContent
}

// NewTextBlock wraps valid text content.
func NewTextBlock(content TextContent) (ContentBlock, error) {
	if !content.Valid() {
		return ContentBlock{}, contentError(CodeInvalidInvariant, "content")
	}
	return ContentBlock{kind: ContentType("text"), text: content}, nil
}

// NewImageBlock wraps valid image content.
func NewImageBlock(content ImageContent) (ContentBlock, error) {
	if !content.Valid() {
		return ContentBlock{}, contentError(CodeInvalidInvariant, "content")
	}
	return ContentBlock{kind: ContentType("image"), image: content}, nil
}

// NewJSONBlock wraps valid JSON content.
func NewJSONBlock(content JSONContent) (ContentBlock, error) {
	if !content.Valid() {
		return ContentBlock{}, contentError(CodeInvalidInvariant, "content")
	}
	return ContentBlock{kind: ContentType("json"), json: content}, nil
}

// Type returns the content discriminator.
func (c ContentBlock) Type() ContentType { return c.kind }

// Text returns text content when this is a text block.
func (c ContentBlock) Text() (TextContent, bool) { return c.text, c.kind == ContentType("text") }

// Image returns image content when this is an image block.
func (c ContentBlock) Image() (ImageContent, bool) { return c.image, c.kind == ContentType("image") }

// JSON returns JSON content when this is a JSON block.
func (c ContentBlock) JSON() (JSONContent, bool) { return c.json, c.kind == ContentType("json") }

// Valid reports whether exactly the selected variant is valid.
func (c ContentBlock) Valid() bool {
	switch c.kind {
	case ContentType("text"):
		return c.text.Valid() && !c.image.Valid() && !c.json.Valid()
	case ContentType("image"):
		return !c.text.Valid() && c.image.Valid() && !c.json.Valid()
	case ContentType("json"):
		return !c.text.Valid() && !c.image.Valid() && c.json.Valid()
	default:
		return false
	}
}
