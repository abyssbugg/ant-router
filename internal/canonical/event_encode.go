package canonical

import "encoding/json"

// EncodeEvent encodes one locally valid canonical event.
func EncodeEvent(event Event) ([]byte, error) {
	if !event.Valid() {
		return nil, codecError(CodeInvalidInvariant, "event")
	}
	data, err := encodeEventData(event)
	if err != nil {
		return nil, err
	}
	dto := eventDTO{Version: ProtocolVersion, ID: event.ID().String(), ResponseID: event.ResponseID().String(), Sequence: event.Sequence(), Type: string(event.Type()), Data: data}
	maximum := MaxOrdinaryEventBytes
	if eventAllowsLargeImage(event) {
		maximum = MaxImageEventBytes
	}
	return marshalCodec(dto, maximum)
}

func encodeEventData(event Event) (json.RawMessage, error) {
	switch event.Type() {
	case EventType("response_started"):
		payload, _ := event.ResponseStarted()
		return marshalCodec(responseStartedDTO{Model: payload.Model()}, MaxOrdinaryEventBytes)
	case EventType("continuation_updated"):
		payload, _ := event.ContinuationUpdated()
		continuation := payload.Continuation()
		encoded := encodeContinuation(&continuation)
		return marshalCodec(continuationUpdatedDTO{Continuation: *encoded}, MaxOrdinaryEventBytes)
	case EventType("item_started"):
		payload, _ := event.ItemStarted()
		return marshalCodec(itemStartedDTO{Index: payload.Index(), ID: payload.ID().String(), ItemType: string(payload.Type()), Role: string(payload.Role()), CallID: payload.CallID().String(), Namespace: payload.Namespace(), Name: payload.Name()}, MaxOrdinaryEventBytes)
	case EventType("content_started"):
		payload, _ := event.ContentStarted()
		return marshalCodec(contentStartedDTO{ItemID: payload.ItemID().String(), ContentIndex: payload.Index(), ContentType: string(payload.Type())}, MaxOrdinaryEventBytes)
	case EventType("text_delta"):
		payload, _ := event.TextDelta()
		return marshalCodec(deltaDTO{ItemID: payload.ItemID().String(), ContentIndex: payload.ContentIndex(), Delta: payload.Delta()}, MaxOrdinaryEventBytes)
	case EventType("json_delta"):
		payload, _ := event.JSONDelta()
		return marshalCodec(deltaDTO{ItemID: payload.ItemID().String(), ContentIndex: payload.ContentIndex(), Delta: payload.Delta()}, MaxOrdinaryEventBytes)
	case EventType("tool_arguments_delta"):
		payload, _ := event.ToolArgumentsDelta()
		return marshalCodec(toolArgumentsDeltaDTO{ItemID: payload.ItemID().String(), Delta: payload.Delta()}, MaxOrdinaryEventBytes)
	case EventType("content_completed"):
		payload, _ := event.ContentCompleted()
		content, err := encodeContent(payload.Content())
		if err != nil {
			return nil, err
		}
		return marshalCodec(contentCompletedDTO{ItemID: payload.ItemID().String(), ContentIndex: payload.Index(), Content: content}, MaxImageEventBytes)
	case EventType("item_completed"):
		payload, _ := event.ItemCompleted()
		item, err := encodeItem(payload.Item())
		if err != nil {
			return nil, err
		}
		return marshalCodec(itemCompletedDTO{Index: payload.Index(), Item: item}, MaxImageEventBytes)
	case EventType("usage_updated"):
		payload, _ := event.UsageUpdated()
		return marshalCodec(usageUpdatedDTO{Usage: encodeUsage(payload.Usage())}, MaxOrdinaryEventBytes)
	case EventType("response_completed"):
		payload, _ := event.ResponseCompleted()
		ids := make([]string, 0, len(payload.ItemIDs()))
		for _, id := range payload.ItemIDs() {
			ids = append(ids, id.String())
		}
		return marshalCodec(responseCompletedDTO{FinishReason: string(payload.FinishReason()), Usage: encodeUsage(payload.Usage()), ItemIDs: ids}, MaxOrdinaryEventBytes)
	case EventType("response_failed"):
		payload, _ := event.ResponseFailed()
		dto := responseFailedDTO{Failure: encodeFailure(payload.Failure())}
		if usage, ok := payload.Usage(); ok {
			encoded := encodeUsage(usage)
			dto.Usage = &encoded
		}
		return marshalCodec(dto, MaxOrdinaryEventBytes)
	case EventType("response_cancelled"):
		payload, _ := event.ResponseCancelled()
		dto := responseFailedDTO{Failure: encodeFailure(payload.Failure())}
		if usage, ok := payload.Usage(); ok {
			encoded := encodeUsage(usage)
			dto.Usage = &encoded
		}
		return marshalCodec(dto, MaxOrdinaryEventBytes)
	default:
		return nil, codecError(CodeInvalidVariant, "event.type")
	}
}

func eventAllowsLargeImage(event Event) bool {
	if payload, ok := event.ContentCompleted(); ok {
		return payload.Content().Type() == ContentType("image")
	}
	if payload, ok := event.ItemCompleted(); ok {
		item := payload.Item()
		if message, ok := item.Message(); ok {
			for _, block := range message.Content() {
				if block.Type() == ContentType("image") {
					return true
				}
			}
		}
	}
	return false
}
