package responses

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

const (
	maxImagePixels        = 100_000_000
	maxEncodedInlineImage = ((canonical.MaxInlineImageBytes + 2) / 3) * 4
)

func decodeImageBlock(fields map[string]json.RawMessage, path string) (canonical.ContentBlock, error) {
	if _, present := fields["file_id"]; present {
		return canonical.ContentBlock{}, newError(CodeUnsupportedField, path+".file_id", errors.New("file ID image input is unsupported"))
	}
	if err := rejectFields(fields, path, "type", "image_url", "detail"); err != nil {
		return canonical.ContentBlock{}, err
	}
	kind, err := requiredString(fields, "type", path+".type")
	if err != nil || kind != "input_image" {
		return canonical.ContentBlock{}, newError(CodeInvalidField, path+".type", errors.New("image content type is invalid"))
	}
	detail := canonical.ImageDetailAuto
	if _, present := fields["detail"]; present {
		value, detailErr := requiredString(fields, "detail", path+".detail")
		if detailErr != nil {
			return canonical.ContentBlock{}, detailErr
		}
		detail = canonical.ImageDetail(value)
		if !detail.Valid() {
			return canonical.ContentBlock{}, newError(CodeInvalidField, path+".detail", errors.New("image detail is invalid"))
		}
	}
	source, err := requiredString(fields, "image_url", path+".image_url")
	if err != nil || source == "" {
		return canonical.ContentBlock{}, newError(CodeInvalidField, path+".image_url", errors.New("image URL is invalid"))
	}
	var image canonical.ImageContent
	if strings.HasPrefix(source, "data:") {
		image, err = decodeDataImage(source, path+".image_url", detail)
	} else {
		image, err = decodeRemoteImage(source, path+".image_url", detail)
	}
	if err != nil {
		return canonical.ContentBlock{}, err
	}
	block, err := canonical.NewImageBlock(image)
	if err != nil {
		return canonical.ContentBlock{}, newError(CodeInvalidField, path+".image_url", errors.New("image input is invalid"))
	}
	return block, nil
}

func decodeRemoteImage(source, path string, detail canonical.ImageDetail) (canonical.ImageContent, error) {
	image, err := canonical.NewRemoteImage(source, "", detail, 0, 0)
	if err != nil {
		return canonical.ImageContent{}, newError(CodeInvalidField, path, errors.New("remote image URL is invalid"))
	}
	return image, nil
}

func decodeDataImage(source, path string, detail canonical.ImageDetail) (canonical.ImageContent, error) {
	if strings.IndexFunc(source, unicode.IsSpace) >= 0 {
		return canonical.ImageContent{}, newError(CodeInvalidField, path, errors.New("image data URL is invalid"))
	}
	metadata, payload, ok := strings.Cut(strings.TrimPrefix(source, "data:"), ",")
	parts := strings.Split(metadata, ";")
	if !ok || len(parts) != 2 || parts[1] != "base64" || payload == "" {
		return canonical.ImageContent{}, newError(CodeInvalidField, path, errors.New("image data URL is invalid"))
	}
	mime := parts[0]
	if !supportedDataImageMIME(mime) {
		return canonical.ImageContent{}, newError(CodeUnsupportedField, path, errors.New("image media type is unsupported"))
	}
	if len(payload) > maxEncodedInlineImage {
		return canonical.ImageContent{}, newError(CodeLimitExceeded, path, errors.New("image data exceeds the limit"))
	}
	data, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil || len(data) == 0 {
		return canonical.ImageContent{}, newError(CodeInvalidField, path, errors.New("image base64 data is invalid"))
	}
	if len(data) > canonical.MaxInlineImageBytes {
		return canonical.ImageContent{}, newError(CodeLimitExceeded, path, errors.New("image data exceeds the limit"))
	}
	width, height, err := imageDimensions(mime, data)
	if err != nil {
		return canonical.ImageContent{}, newError(CodeInvalidField, path, errors.New("image header is invalid"))
	}
	if width <= 0 || height <= 0 || width > canonical.MaxImageDimension || height > canonical.MaxImageDimension || uint64(width)*uint64(height) > maxImagePixels {
		return canonical.ImageContent{}, newError(CodeLimitExceeded, path, errors.New("image dimensions exceed the limit"))
	}
	image, err := canonical.NewInlineImage(data, mime, detail, width, height)
	if err != nil {
		return canonical.ImageContent{}, newError(CodeInvalidField, path, errors.New("inline image is invalid"))
	}
	return image, nil
}

func supportedDataImageMIME(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func imageDimensions(mime string, data []byte) (int, int, error) {
	var width, height int
	var err error
	switch mime {
	case "image/png":
		if len(data) < 24 || string(data[:8]) != "\x89PNG\r\n\x1a\n" || binary.BigEndian.Uint32(data[8:12]) != 13 || string(data[12:16]) != "IHDR" {
			return 0, 0, errors.New("invalid PNG header")
		}
		width = int(binary.BigEndian.Uint32(data[16:20]))
		height = int(binary.BigEndian.Uint32(data[20:24]))
	case "image/jpeg":
		width, height, err = jpegDimensions(data)
	case "image/gif":
		if len(data) < 10 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
			return 0, 0, errors.New("invalid GIF header")
		}
		width = int(binary.LittleEndian.Uint16(data[6:8]))
		height = int(binary.LittleEndian.Uint16(data[8:10]))
	case "image/webp":
		width, height, err = webpDimensions(data)
	default:
		return 0, 0, errors.New("unsupported image header")
	}
	if err != nil {
		return 0, 0, err
	}
	if width <= 0 || height <= 0 {
		return 0, 0, errors.New("image dimensions are invalid")
	}
	return width, height, nil
}

func jpegDimensions(data []byte) (int, int, error) {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 || data[2] != 0xff {
		return 0, 0, errors.New("invalid JPEG header")
	}
	for offset := 2; offset < len(data); {
		if data[offset] != 0xff {
			return 0, 0, errors.New("invalid JPEG marker")
		}
		for offset < len(data) && data[offset] == 0xff {
			offset++
		}
		if offset >= len(data) {
			break
		}
		marker := data[offset]
		offset++
		if marker == 0xd8 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if marker == 0xd9 || marker == 0xda || offset+2 > len(data) {
			break
		}
		length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		if length < 2 || offset+length > len(data) {
			return 0, 0, errors.New("truncated JPEG segment")
		}
		if isJPEGStartOfFrame(marker) {
			if length < 7 {
				return 0, 0, errors.New("invalid JPEG frame")
			}
			return int(binary.BigEndian.Uint16(data[offset+5 : offset+7])), int(binary.BigEndian.Uint16(data[offset+3 : offset+5])), nil
		}
		offset += length
	}
	return 0, 0, errors.New("JPEG dimensions not found")
}

func isJPEGStartOfFrame(marker byte) bool {
	switch marker {
	case 0xc0, 0xc1, 0xc2, 0xc3, 0xc5, 0xc6, 0xc7, 0xc9, 0xca, 0xcb, 0xcd, 0xce, 0xcf:
		return true
	default:
		return false
	}
}

func webpDimensions(data []byte) (int, int, error) {
	if len(data) < 20 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, errors.New("invalid WebP header")
	}
	chunkSize := binary.LittleEndian.Uint32(data[16:20])
	if uint64(chunkSize) > uint64(len(data)-20) {
		return 0, 0, errors.New("truncated WebP chunk")
	}
	switch string(data[12:16]) {
	case "VP8X":
		if chunkSize < 10 || len(data) < 30 {
			return 0, 0, errors.New("invalid VP8X header")
		}
		width := 1 + int(data[24]) + int(data[25])<<8 + int(data[26])<<16
		height := 1 + int(data[27]) + int(data[28])<<8 + int(data[29])<<16
		return width, height, nil
	case "VP8L":
		if chunkSize < 5 || len(data) < 25 || data[20] != 0x2f {
			return 0, 0, errors.New("invalid VP8L header")
		}
		bits := binary.LittleEndian.Uint32(data[21:25])
		return 1 + int(bits&0x3fff), 1 + int((bits>>14)&0x3fff), nil
	case "VP8 ":
		if chunkSize < 10 || len(data) < 30 || data[23] != 0x9d || data[24] != 0x01 || data[25] != 0x2a {
			return 0, 0, errors.New("invalid VP8 header")
		}
		width := int(binary.LittleEndian.Uint16(data[26:28]) & 0x3fff)
		height := int(binary.LittleEndian.Uint16(data[28:30]) & 0x3fff)
		return width, height, nil
	default:
		return 0, 0, errors.New("unsupported WebP chunk")
	}
}
