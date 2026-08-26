package canonicaljson

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
)

func encodeNode(value node) ([]byte, error) {
	var output bytes.Buffer
	if err := writeNode(&output, value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeNode(output *bytes.Buffer, value node) error {
	switch value.kind {
	case KindNull:
		output.WriteString("null")
	case KindBool:
		if value.boolean {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case KindNumber:
		output.WriteString(value.text)
	case KindString:
		encoded, err := json.Marshal(value.text)
		if err != nil {
			return err
		}
		output.Write(encoded)
	case KindArray:
		output.WriteByte('[')
		for index, child := range value.array {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := writeNode(output, child); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case KindObject:
		members := append([]member(nil), value.object...)
		sort.Slice(members, func(i, j int) bool { return members[i].key < members[j].key })
		output.WriteByte('{')
		for index, item := range members {
			if index > 0 {
				output.WriteByte(',')
			}
			key, err := json.Marshal(item.key)
			if err != nil {
				return err
			}
			output.Write(key)
			output.WriteByte(':')
			if err := writeNode(output, item.value); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return errors.New("invalid JSON node")
	}
	return nil
}
