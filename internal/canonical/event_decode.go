package canonical

func DecodeEvent(data []byte) (Event, error) {
	prepared, err := prepareCodecInput(data, MaxImageEventBytes)
	if err != nil {
		return Event{}, err
	}
	var dto eventDTO
	if err := strictDecode(prepared, &dto); err != nil {
		return Event{}, err
	}
	if dto.Version != ProtocolVersion {
		return Event{}, codecError(CodeInvalidVersion, "version")
	}
	id, err := ParseEventID(dto.ID)
	if err != nil {
		return Event{}, err
	}
	responseID, err := ParseResponseID(dto.ResponseID)
	if err != nil {
		return Event{}, err
	}
	kind := EventType(dto.Type)
	if !kind.Valid() {
		return Event{}, codecError(CodeInvalidVariant, "event.type")
	}
	var event Event
	switch kind {
	case EventType("response_started"):
		var payload responseStartedDTO
		if err := strictDecode(dto.Data, &payload); err != nil {
			return Event{}, err
		}
		event, err = NewResponseStartedEvent(id, responseID, dto.Sequence, payload.Model)
	case EventType("item_started"):
		var payload itemStartedDTO
		if err := strictDecode(dto.Data, &payload); err != nil {
			return Event{}, err
		}
		itemID, parseErr := ParseItemID(payload.ID)
		if parseErr != nil {
			return Event{}, parseErr
		}
		var callID ToolCallID
		if payload.CallID != "" {
			callID, parseErr = ParseToolCallID(payload.CallID)
			if parseErr != nil {
				return Event{}, parseErr
			}
		}
		event, err = NewItemStartedEvent(id, responseID, dto.Sequence, ItemStartParams{Index: payload.Index, ID: itemID, Type: ItemType(payload.ItemType), Role: Role(payload.Role), CallID: callID, Name: payload.Name})
	case EventType("content_started"):
		var payload contentStartedDTO
		if err := strictDecode(dto.Data, &payload); err != nil {
			return Event{}, err
		}
		itemID, parseErr := ParseItemID(payload.ItemID)
		if parseErr != nil {
			return Event{}, parseErr
		}
		event, err = NewContentStartedEvent(id, responseID, dto.Sequence, itemID, payload.ContentIndex, ContentType(payload.ContentType))
	case EventType("text_delta"), EventType("json_delta"):
		var payload deltaDTO
		if err := strictDecode(dto.Data, &payload); err != nil {
			return Event{}, err
		}
		itemID, parseErr := ParseItemID(payload.ItemID)
		if parseErr != nil {
			return Event{}, parseErr
		}
		if kind == EventType("text_delta") {
			event, err = NewTextDeltaEvent(id, responseID, dto.Sequence, itemID, payload.ContentIndex, payload.Delta)
		} else {
			event, err = NewJSONDeltaEvent(id, responseID, dto.Sequence, itemID, payload.ContentIndex, payload.Delta)
		}
	case EventType("tool_arguments_delta"):
		var payload toolArgumentsDeltaDTO
		if err := strictDecode(dto.Data, &payload); err != nil {
			return Event{}, err
		}
		itemID, parseErr := ParseItemID(payload.ItemID)
		if parseErr != nil {
			return Event{}, parseErr
		}
		event, err = NewToolArgumentsDeltaEvent(id, responseID, dto.Sequence, itemID, payload.Delta)
	case EventType("content_completed"):
		var payload contentCompletedDTO
		if err := strictDecode(dto.Data, &payload); err != nil {
			return Event{}, err
		}
		itemID, parseErr := ParseItemID(payload.ItemID)
		if parseErr != nil {
			return Event{}, parseErr
		}
		content, parseErr := decodeContent(payload.Content)
		if parseErr != nil {
			return Event{}, parseErr
		}
		event, err = NewContentCompletedEvent(id, responseID, dto.Sequence, itemID, payload.ContentIndex, content)
	case EventType("item_completed"):
		var payload itemCompletedDTO
		if err := strictDecode(dto.Data, &payload); err != nil {
			return Event{}, err
		}
		item, parseErr := decodeItem(payload.Item)
		if parseErr != nil {
			return Event{}, parseErr
		}
		event, err = NewItemCompletedEvent(id, responseID, dto.Sequence, payload.Index, item)
	case EventType("usage_updated"):
		var payload usageUpdatedDTO
		if err := strictDecode(dto.Data, &payload); err != nil {
			return Event{}, err
		}
		usage, parseErr := decodeUsage(payload.Usage)
		if parseErr != nil {
			return Event{}, parseErr
		}
		event, err = NewUsageUpdatedEvent(id, responseID, dto.Sequence, usage)
	case EventType("response_completed"):
		var payload responseCompletedDTO
		if err := strictDecode(dto.Data, &payload); err != nil {
			return Event{}, err
		}
		usage, parseErr := decodeUsage(payload.Usage)
		if parseErr != nil {
			return Event{}, parseErr
		}
		itemIDs := make([]ItemID, 0, len(payload.ItemIDs))
		for _, raw := range payload.ItemIDs {
			itemID, parseErr := ParseItemID(raw)
			if parseErr != nil {
				return Event{}, parseErr
			}
			itemIDs = append(itemIDs, itemID)
		}
		event, err = NewResponseCompletedEvent(id, responseID, dto.Sequence, FinishReason(payload.FinishReason), usage, itemIDs)
	case EventType("response_failed"), EventType("response_cancelled"):
		var payload responseFailedDTO
		if err := strictDecode(dto.Data, &payload); err != nil {
			return Event{}, err
		}
		failure, parseErr := decodeFailure(payload.Failure)
		if parseErr != nil {
			return Event{}, parseErr
		}
		var usage *Usage
		if payload.Usage != nil {
			decodedUsage, usageErr := decodeUsage(*payload.Usage)
			if usageErr != nil {
				return Event{}, usageErr
			}
			usage = &decodedUsage
		}
		if kind == EventType("response_failed") {
			event, err = NewResponseFailedEvent(id, responseID, dto.Sequence, failure, usage)
		} else {
			event, err = NewResponseCancelledEvent(id, responseID, dto.Sequence, failure, usage)
		}
	default:
		return Event{}, codecError(CodeInvalidVariant, "event.type")
	}
	if err != nil {
		return Event{}, err
	}
	if len(prepared) > MaxOrdinaryEventBytes && !eventAllowsLargeImage(event) {
		return Event{}, codecError(CodeLimitExceeded, "event")
	}
	return event, nil
}
