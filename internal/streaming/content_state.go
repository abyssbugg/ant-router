package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

type contentState struct {
	index        int
	kind         canonical.ContentType
	buffer       []byte
	citations    []canonical.URLCitation
	completed    bool
	finalContent canonical.ContentBlock
}

func (s *contentState) clone() *contentState {
	if s == nil {
		return nil
	}
	copyValue := *s
	copyValue.buffer = append([]byte(nil), s.buffer...)
	copyValue.citations = append([]canonical.URLCitation(nil), s.citations...)
	return &copyValue
}
