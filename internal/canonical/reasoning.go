package canonical

// ReasoningSummary is immutable user-visible reasoning summary text.
type ReasoningSummary struct {
	id        ItemID
	summaries []TextContent
}

// NewReasoningSummary validates and copies summary text blocks.
func NewReasoningSummary(id ItemID, summaries []TextContent) (ReasoningSummary, error) {
	if !id.Valid() {
		return ReasoningSummary{}, itemError(CodeInvalidID, "reasoning_summary.id")
	}
	if len(summaries) > MaxContentBlocks {
		return ReasoningSummary{}, itemError(CodeLimitExceeded, "reasoning_summary.content")
	}
	for _, summary := range summaries {
		if !summary.Valid() {
			return ReasoningSummary{}, itemError(CodeInvalidInvariant, "reasoning_summary.content")
		}
	}
	return ReasoningSummary{id: id, summaries: append([]TextContent(nil), summaries...)}, nil
}

func (r ReasoningSummary) ID() ItemID { return r.id }
func (r ReasoningSummary) Summaries() []TextContent {
	return append([]TextContent(nil), r.summaries...)
}
func (r ReasoningSummary) Valid() bool {
	if !r.id.Valid() || len(r.summaries) > MaxContentBlocks {
		return false
	}
	for _, summary := range r.summaries {
		if !summary.Valid() {
			return false
		}
	}
	return true
}
