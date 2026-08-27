package streaming

import "testing"

func TestTransitionAccessors(t *testing.T) {
	event := simpleEvent(t)
	response := simpleResponse(t)
	failure := simpleFailure(t)
	transition := newTransition(event, true, true, TerminalCompleted, &response, &failure, nil)
	if transition.Event().ID() != event.ID() || !transition.SemanticOutput() || !transition.JustCommitted() || transition.TerminalKind() != TerminalCompleted {
		t.Errorf("transition=%#v", transition)
	}
	if got, ok := transition.Response(); !ok || got.ID() != response.ID() {
		t.Error("response missing")
	}
	if got, ok := transition.Failure(); !ok || got.Code() != failure.Code() {
		t.Error("failure missing")
	}
	if _, ok := transition.Usage(); ok {
		t.Error("unexpected usage")
	}
}
