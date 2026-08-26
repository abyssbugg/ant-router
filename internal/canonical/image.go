package canonical

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// ImageSourceType identifies a remote or inline image source.
type ImageSourceType string

const (
	ImageSourceURL    ImageSourceType = "url"
	ImageSourceInline ImageSourceType = "inline"
)

// ImageContent is an immutable validated canonical image.
type ImageContent struct {
	source ImageSourceType
	url    string
	data   []byte
	mime   string
	detail ImageDetail
	width  int
	height int
}

// NewRemoteImage constructs an HTTPS-backed image.
func NewRemoteImage(rawURL, mime string, detail ImageDetail, width, height int) (ImageContent, error) {
	if !utf8.ValidString(rawURL) {
		return ImageContent{}, contentError(CodeInvalidURL, "content.image.url")
	}
	if len(rawURL) > MaxImageURLBytes {
		return ImageContent{}, contentError(CodeLimitExceeded, "content.image.url")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return ImageContent{}, contentError(CodeInvalidURL, "content.image.url")
	}
	image := ImageContent{source: ImageSourceURL, url: parsed.String(), mime: mime, detail: detail, width: width, height: height}
	if err := image.validate(); err != nil {
		return ImageContent{}, err
	}
	return image, nil
}

// NewInlineImage constructs an image from bounded inline bytes.
func NewInlineImage(data []byte, mime string, detail ImageDetail, width, height int) (ImageContent, error) {
	if len(data) == 0 {
		return ImageContent{}, contentError(CodeInvalidInvariant, "content.image.data")
	}
	if len(data) > MaxInlineImageBytes {
		return ImageContent{}, contentError(CodeLimitExceeded, "content.image.data")
	}
	image := ImageContent{source: ImageSourceInline, data: append([]byte(nil), data...), mime: mime, detail: detail, width: width, height: height}
	if err := image.validate(); err != nil {
		return ImageContent{}, err
	}
	return image, nil
}

// SourceType returns the image source variant.
func (i ImageContent) SourceType() ImageSourceType { return i.source }

// URL returns the remote URL when the source is URL.
func (i ImageContent) URL() (string, bool) { return i.url, i.source == ImageSourceURL }

// Bytes returns a defensive inline byte copy when the source is inline.
func (i ImageContent) Bytes() ([]byte, bool) {
	if i.source != ImageSourceInline {
		return nil, false
	}
	return append([]byte(nil), i.data...), true
}

// MIME returns the validated image MIME type.
func (i ImageContent) MIME() string { return i.mime }

// Detail returns the provider-neutral image detail hint.
func (i ImageContent) Detail() ImageDetail { return i.detail }

// Width returns the optional image width.
func (i ImageContent) Width() int { return i.width }

// Height returns the optional image height.
func (i ImageContent) Height() int { return i.height }

// Valid reports whether the image satisfies canonical invariants.
func (i ImageContent) Valid() bool { return i.validate() == nil }

func (i ImageContent) validate() error {
	if i.source != ImageSourceURL && i.source != ImageSourceInline {
		return contentError(CodeInvalidInvariant, "content.image.source")
	}
	if i.source == ImageSourceURL && (i.url == "" || len(i.data) != 0) {
		return contentError(CodeInvalidInvariant, "content.image.source")
	}
	if i.source == ImageSourceInline && (len(i.data) == 0 || i.url != "") {
		return contentError(CodeInvalidInvariant, "content.image.source")
	}
	if !supportedImageMIME(i.mime) {
		return contentError(CodeUnsupportedMediaType, "content.image.mime")
	}
	if !i.detail.Valid() {
		return contentError(CodeInvalidInvariant, "content.image.detail")
	}
	if (i.width == 0) != (i.height == 0) || i.width < 0 || i.height < 0 {
		return contentError(CodeInvalidInvariant, "content.image.dimensions")
	}
	if i.width > MaxImageDimension || i.height > MaxImageDimension {
		return contentError(CodeLimitExceeded, "content.image.dimensions")
	}
	return nil
}

func supportedImageMIME(value string) bool {
	return value == "image/png" || value == "image/jpeg" || value == "image/webp" || value == "image/gif"
}
