package streaming

import (
	"bytes"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// acceptContinuationUpdated accepts cumulative updates in both precommit and
// committed states; delivery phase is owned by Pump.
func (m *Machine) acceptContinuationUpdated(event canonical.Event) error {
	payload, ok := event.ContinuationUpdated()
	if !ok || !payload.Valid() {
		return m.error(CodeInvalidItemState, "event.continuation_updated")
	}
	continuation := payload.Continuation()
	if m.continuation != nil {
		if continuation.Provider() != m.continuation.Provider() || continuation.Model() != m.continuation.Model() {
			return m.error(CodeInvalidItemState, "event.continuation_updated.affinity")
		}
		if bytes.Equal(continuation.Bytes(), m.continuation.Bytes()) {
			return m.error(CodeInvalidItemState, "event.continuation_updated")
		}
	}
	data := continuation.Bytes()
	copyValue, err := canonical.NewContinuation(continuation.Provider(), continuation.Model(), data)
	if err != nil {
		return m.error(CodeInvalidItemState, "event.continuation_updated")
	}
	m.continuation = &copyValue
	m.continuationBytes = len(data)
	return nil
}
