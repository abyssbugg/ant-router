package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

// Transition describes one atomically accepted machine event.
type Transition struct {
	event          canonical.Event
	semanticOutput bool
	justCommitted  bool
	terminal       TerminalKind
	response       canonical.Response
	hasResponse    bool
	failure        canonical.Failure
	hasFailure     bool
	usage          canonical.Usage
	hasUsage       bool
}

func newTransition(event canonical.Event, semantic, committed bool, terminal TerminalKind, response *canonical.Response, failure *canonical.Failure, usage *canonical.Usage) Transition {
	result := Transition{event: event, semanticOutput: semantic, justCommitted: committed, terminal: terminal}
	if response != nil {
		result.response, result.hasResponse = *response, true
	}
	if failure != nil {
		result.failure, result.hasFailure = *failure, true
	}
	if usage != nil {
		result.usage, result.hasUsage = *usage, true
	}
	return result
}

// Event returns the accepted canonical event.
func (t Transition) Event() canonical.Event { return t.event }

// SemanticOutput reports whether the event carries semantic output.
func (t Transition) SemanticOutput() bool { return t.semanticOutput }

// JustCommitted reports whether this event crossed the commit point.
func (t Transition) JustCommitted() bool { return t.justCommitted }

// TerminalKind returns the terminal outcome caused by this event.
func (t Transition) TerminalKind() TerminalKind { return t.terminal }

// Response returns the reconstructed response when present.
func (t Transition) Response() (canonical.Response, bool) { return t.response, t.hasResponse }

// Failure returns the canonical failure when present.
func (t Transition) Failure() (canonical.Failure, bool) { return t.failure, t.hasFailure }

// Usage returns terminal or updated usage when present.
func (t Transition) Usage() (canonical.Usage, bool) { return t.usage, t.hasUsage }
