package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

type itemState struct {
	index                int
	id                   canonical.ItemID
	kind                 canonical.ItemType
	role                 canonical.Role
	callID               canonical.ToolCallID
	namespace            string
	name                 string
	contents             map[int]*contentState
	nextContent          int
	arguments            []byte
	customInput          []byte
	customInputCompleted bool
	searchStatus         canonical.HostedSearchStatus
	searchAction         canonical.HostedSearchAction
	completed            bool
	finalItem            canonical.Item
}

func newItemState(payload canonical.ItemStartedPayload) *itemState {
	return &itemState{index: payload.Index(), id: payload.ID(), kind: payload.Type(), role: payload.Role(), callID: payload.CallID(), namespace: payload.Namespace(), name: payload.Name(), contents: make(map[int]*contentState), searchStatus: payload.SearchStatus(), searchAction: payload.SearchAction()}
}

func (s *itemState) clone() *itemState {
	if s == nil {
		return nil
	}
	copyValue := *s
	copyValue.arguments = append([]byte(nil), s.arguments...)
	copyValue.customInput = append([]byte(nil), s.customInput...)
	copyValue.contents = make(map[int]*contentState, len(s.contents))
	for index, content := range s.contents {
		copyValue.contents[index] = content.clone()
	}
	return &copyValue
}
