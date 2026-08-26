package canonical

import "encoding/json"

// EncodeResponse encodes a validated completed canonical response.
func EncodeResponse(response Response) ([]byte, error) {
	if !response.Valid() {
		return nil, codecError(CodeInvalidInvariant, "response")
	}
	items := make([]json.RawMessage, 0, len(response.Items()))
	for _, item := range response.Items() {
		encoded, err := encodeItem(item)
		if err != nil {
			return nil, err
		}
		items = append(items, encoded)
	}
	dto := responseDTO{Version: ProtocolVersion, ID: response.ID().String(), Model: response.Model(), Items: items, FinishReason: string(response.FinishReason()), Usage: encodeUsage(response.Usage())}
	return marshalCodec(dto, MaxRequestBytes)
}

// DecodeResponse strictly decodes one completed canonical response.
func DecodeResponse(data []byte) (Response, error) {
	prepared, err := prepareCodecInput(data, MaxRequestBytes)
	if err != nil {
		return Response{}, err
	}
	var dto responseDTO
	if err := strictDecode(prepared, &dto); err != nil {
		return Response{}, err
	}
	if dto.Version != ProtocolVersion {
		return Response{}, codecError(CodeInvalidVersion, "version")
	}
	id, err := ParseResponseID(dto.ID)
	if err != nil {
		return Response{}, err
	}
	items := make([]Item, 0, len(dto.Items))
	for _, raw := range dto.Items {
		item, err := decodeItem(raw)
		if err != nil {
			return Response{}, err
		}
		items = append(items, item)
	}
	usage, err := decodeUsage(dto.Usage)
	if err != nil {
		return Response{}, err
	}
	return NewResponse(ResponseParams{ID: id, Model: dto.Model, Items: items, FinishReason: FinishReason(dto.FinishReason), Usage: usage})
}
