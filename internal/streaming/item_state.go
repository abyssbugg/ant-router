package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

type itemState struct {
	index       int
	id          canonical.ItemID
	kind        canonical.ItemType
	role        canonical.Role
	callID      canonical.ToolCallID
	name        string
	contents    map[int]*contentState
	nextContent int
	completed   bool
}

func newItemState(payload canonical.ItemStartedPayload) *itemState {
	return &itemState{index: payload.Index(), id: payload.ID(), kind: payload.Type(), role: payload.Role(), callID: payload.CallID(), name: payload.Name(), contents: make(map[int]*contentState)}
}
