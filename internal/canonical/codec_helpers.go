package canonical

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func prepareCodecInput(data []byte, maximum int) ([]byte, error) {
	if len(data) > maximum {
		return nil, codecError(CodeLimitExceeded, "document")
	}
	value, err := canonicaljson.Parse(data, canonicaljson.Limits{MaximumBytes: maximum, MaximumDepth: MaxJSONDepth})
	if err != nil {
		code, _ := canonicaljson.CodeOf(err)
		switch code {
		case canonicaljson.CodeInvalidUTF8:
			return nil, codecError(CodeInvalidUTF8, "document")
		case canonicaljson.CodeDuplicateKey:
			return nil, codecError(CodeDuplicateKey, "document")
		case canonicaljson.CodeLimitExceeded:
			return nil, codecError(CodeLimitExceeded, "document")
		default:
			return nil, codecError(CodeInvalidJSON, "document")
		}
	}
	if !value.IsObject() {
		return nil, codecError(CodeInvalidJSON, "document")
	}
	return value.Bytes(), nil
}

func strictDecode(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return codecError(CodeUnknownField, "document")
		}
		if strings.Contains(err.Error(), "base64") {
			return codecError(CodeInvalidInvariant, "document")
		}
		return codecError(CodeInvalidJSON, "document")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return codecError(CodeInvalidJSON, "document")
	}
	return nil
}

func discriminator(data []byte) (string, error) {
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &header); err != nil || header.Type == "" {
		return "", codecError(CodeInvalidVariant, "type")
	}
	return header.Type, nil
}

func marshalCodec(value any, maximum int) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, codecError(CodeInvalidInvariant, "document")
	}
	if len(encoded) > maximum {
		return nil, codecError(CodeLimitExceeded, "document")
	}
	return encoded, nil
}

func codecError(code ErrorCode, path string) error {
	return &Error{Code: code, Path: path, Err: errors.New("canonical document is invalid")}
}
