package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestMachineEmptyCompletedResponseCommits(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	usage := canonical.Usage{}
	completed, _ := canonical.NewResponseCompletedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.FinishReason("content_filter"), usage, nil)
	transition := b.mustAccept(completed)
	if !transition.JustCommitted() || transition.TerminalKind() != TerminalCompleted || b.machine.State() != StateTerminal {
		t.Errorf("transition=%#v state=%q", transition, b.machine.State())
	}
	if response, ok := transition.Response(); !ok || !response.Valid() || len(response.Items()) != 0 {
		t.Errorf("response=%#v, %v", response, ok)
	}
}

func TestMachinePrecommitAndPostcommitFailures(t *testing.T) {
	failure, _ := canonical.NewFailure(canonical.FailureParams{Code: canonical.FailureCode("internal_error"), Message: "internal error", Retryable: true})
	precommit := newStreamBuilder(t, DefaultMachineOptions())
	failed, _ := canonical.NewResponseFailedEvent(precommit.nextEventID(), precommit.responseID, precommit.sequence, failure, nil)
	transition := precommit.mustAccept(failed)
	if transition.JustCommitted() || precommit.machine.Snapshot().Committed() || transition.TerminalKind() != TerminalFailed {
		t.Errorf("precommit failure=%#v", transition)
	}

	postcommit := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 1)
	start, _ := canonical.NewItemStartedEvent(postcommit.nextEventID(), postcommit.responseID, postcommit.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	postcommit.sequence++
	postcommit.mustAccept(start)
	content, _ := canonical.NewContentStartedEvent(postcommit.nextEventID(), postcommit.responseID, postcommit.sequence, itemID, 0, canonical.ContentType("text"))
	postcommit.sequence++
	postcommit.mustAccept(content)
	delta, _ := canonical.NewTextDeltaEvent(postcommit.nextEventID(), postcommit.responseID, postcommit.sequence, itemID, 0, "x")
	postcommit.sequence++
	postcommit.mustAccept(delta)
	failed, _ = canonical.NewResponseFailedEvent(postcommit.nextEventID(), postcommit.responseID, postcommit.sequence, failure, nil)
	transition = postcommit.mustAccept(failed)
	if transition.TerminalKind() != TerminalFailed || postcommit.machine.State() != StateTerminal || !postcommit.machine.Snapshot().Committed() {
		t.Errorf("postcommit failure=%#v", transition)
	}
}

func TestMachineCancelledTerminalRequiresCancelledFailure(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	cancelFailure, _ := canonical.NewFailure(canonical.FailureParams{Code: canonical.FailureCode("cancelled"), Message: "cancelled"})
	cancelled, _ := canonical.NewResponseCancelledEvent(b.nextEventID(), b.responseID, b.sequence, cancelFailure, nil)
	transition := b.mustAccept(cancelled)
	if transition.TerminalKind() != TerminalCancelled || transition.JustCommitted() {
		t.Errorf("cancelled transition=%#v", transition)
	}
	if _, err := b.machine.Accept(cancelled); codeOfStream(err) != CodeEventAfterTerminal {
		t.Errorf("event-after-terminal code=%q", codeOfStream(err))
	}
}
