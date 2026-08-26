package canonical

import (
	"encoding/json"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func encodeContent(block ContentBlock) (json.RawMessage, error) {
	if !block.Valid() {
		return nil, codecError(CodeInvalidInvariant, "content")
	}
	switch block.Type() {
	case ContentType("text"):
		content, _ := block.Text()
		return marshalCodec(textContentDTO{Type: "text", Text: content.Text()}, MaxRequestBytes)
	case ContentType("image"):
		content, _ := block.Image()
		source := imageSourceDTO{Type: string(content.SourceType())}
		if urlValue, ok := content.URL(); ok {
			source.URL = urlValue
		} else {
			source.Data, _ = content.Bytes()
		}
		return marshalCodec(imageContentDTO{Type: "image", Source: source, MIMEType: content.MIME(), Detail: string(content.Detail()), Width: content.Width(), Height: content.Height()}, MaxRequestBytes)
	case ContentType("json"):
		content, _ := block.JSON()
		return marshalCodec(jsonContentDTO{Type: "json", Value: json.RawMessage(content.Value().Bytes())}, MaxRequestBytes)
	default:
		return nil, codecError(CodeInvalidVariant, "content.type")
	}
}

func decodeContent(data []byte) (ContentBlock, error) {
	kind, err := discriminator(data)
	if err != nil {
		return ContentBlock{}, err
	}
	switch kind {
	case "text":
		var dto textContentDTO
		if err := strictDecode(data, &dto); err != nil {
			return ContentBlock{}, err
		}
		content, err := NewTextContent(dto.Text)
		if err != nil {
			return ContentBlock{}, err
		}
		return NewTextBlock(content)
	case "image":
		var dto imageContentDTO
		if err := strictDecode(data, &dto); err != nil {
			return ContentBlock{}, err
		}
		if dto.Source.Type == "url" {
			if dto.Source.URL == "" || len(dto.Source.Data) != 0 {
				return ContentBlock{}, codecError(CodeInvalidInvariant, "content.image.source")
			}
			content, err := NewRemoteImage(dto.Source.URL, dto.MIMEType, ImageDetail(dto.Detail), dto.Width, dto.Height)
			if err != nil {
				return ContentBlock{}, err
			}
			return NewImageBlock(content)
		}
		if dto.Source.Type == "inline" {
			if len(dto.Source.Data) == 0 || dto.Source.URL != "" {
				return ContentBlock{}, codecError(CodeInvalidInvariant, "content.image.source")
			}
			content, err := NewInlineImage(dto.Source.Data, dto.MIMEType, ImageDetail(dto.Detail), dto.Width, dto.Height)
			if err != nil {
				return ContentBlock{}, err
			}
			return NewImageBlock(content)
		}
		return ContentBlock{}, codecError(CodeInvalidVariant, "content.image.source.type")
	case "json":
		var dto jsonContentDTO
		if err := strictDecode(data, &dto); err != nil {
			return ContentBlock{}, err
		}
		value, err := canonicaljson.Parse(dto.Value, canonicaljson.Limits{MaximumBytes: MaxJSONBytes, MaximumDepth: MaxJSONDepth})
		if err != nil {
			return ContentBlock{}, codecError(CodeInvalidJSON, "content.json")
		}
		content, err := NewJSONContent(value)
		if err != nil {
			return ContentBlock{}, err
		}
		return NewJSONBlock(content)
	default:
		return ContentBlock{}, codecError(CodeInvalidVariant, "content.type")
	}
}
