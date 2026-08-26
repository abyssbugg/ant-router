package canonicaljson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

type node struct {
	kind    Kind
	text    string
	boolean bool
	array   []node
	object  []member
}

type member struct {
	key   string
	value node
}

// Parse validates and deterministically encodes exactly one JSON value.
func Parse(data []byte, limits Limits) (Value, error) {
	if limits.MaximumBytes <= 0 || limits.MaximumDepth <= 0 {
		return Value{}, newError(CodeLimitExceeded)
	}
	if len(data) > limits.MaximumBytes {
		return Value{}, newError(CodeLimitExceeded)
	}
	if !utf8.Valid(data) {
		return Value{}, newError(CodeInvalidUTF8)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	parsed, err := parseNode(decoder, 0, limits.MaximumDepth)
	if err != nil {
		return Value{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Value{}, newError(CodeInvalidJSON)
	}
	encoded, err := encodeNode(parsed)
	if err != nil {
		return Value{}, newError(CodeInvalidJSON)
	}
	return Value{canonical: encoded, kind: parsed.kind}, nil
}

func parseNode(decoder *json.Decoder, depth, maximumDepth int) (node, error) {
	token, err := decoder.Token()
	if err != nil {
		return node{}, newError(CodeInvalidJSON)
	}
	switch value := token.(type) {
	case nil:
		return node{kind: KindNull}, nil
	case bool:
		return node{kind: KindBool, boolean: value}, nil
	case string:
		return node{kind: KindString, text: value}, nil
	case json.Number:
		return node{kind: KindNumber, text: value.String()}, nil
	case json.Delim:
		containerDepth := depth + 1
		if containerDepth > maximumDepth {
			return node{}, newError(CodeLimitExceeded)
		}
		switch value {
		case '[':
			var values []node
			for decoder.More() {
				child, err := parseNode(decoder, containerDepth, maximumDepth)
				if err != nil {
					return node{}, err
				}
				values = append(values, child)
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return node{}, newError(CodeInvalidJSON)
			}
			return node{kind: KindArray, array: values}, nil
		case '{':
			seen := make(map[string]struct{})
			var values []member
			for decoder.More() {
				keyToken, err := decoder.Token()
				key, ok := keyToken.(string)
				if err != nil || !ok {
					return node{}, newError(CodeInvalidJSON)
				}
				if _, exists := seen[key]; exists {
					return node{}, newError(CodeDuplicateKey)
				}
				seen[key] = struct{}{}
				child, err := parseNode(decoder, containerDepth, maximumDepth)
				if err != nil {
					return node{}, err
				}
				values = append(values, member{key: key, value: child})
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return node{}, newError(CodeInvalidJSON)
			}
			return node{kind: KindObject, object: values}, nil
		default:
			return node{}, newError(CodeInvalidJSON)
		}
	default:
		return node{}, newError(CodeInvalidJSON)
	}
}
