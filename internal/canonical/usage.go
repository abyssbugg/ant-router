package canonical

// UsageParams contains canonical token counters for NewUsage.
type UsageParams struct {
	InputTokens      int64
	OutputTokens     int64
	ReasoningTokens  int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	TotalTokens      int64
}

// Usage is immutable canonical token accounting. Reasoning is a subset of
// output tokens; cache counters are subsets of input tokens.
type Usage struct {
	inputTokens      int64
	outputTokens     int64
	reasoningTokens  int64
	cacheReadTokens  int64
	cacheWriteTokens int64
	totalTokens      int64
}

// NewUsage validates canonical usage arithmetic.
func NewUsage(params UsageParams) (Usage, error) {
	usage := Usage{
		inputTokens:      params.InputTokens,
		outputTokens:     params.OutputTokens,
		reasoningTokens:  params.ReasoningTokens,
		cacheReadTokens:  params.CacheReadTokens,
		cacheWriteTokens: params.CacheWriteTokens,
		totalTokens:      params.TotalTokens,
	}
	if !usage.Valid() {
		return Usage{}, responseError(CodeInvalidInvariant, "usage")
	}
	return usage, nil
}

func (u Usage) InputTokens() int64      { return u.inputTokens }
func (u Usage) OutputTokens() int64     { return u.outputTokens }
func (u Usage) ReasoningTokens() int64  { return u.reasoningTokens }
func (u Usage) CacheReadTokens() int64  { return u.cacheReadTokens }
func (u Usage) CacheWriteTokens() int64 { return u.cacheWriteTokens }
func (u Usage) TotalTokens() int64      { return u.totalTokens }
func (u Usage) Valid() bool {
	return u.inputTokens >= 0 && u.outputTokens >= 0 && u.reasoningTokens >= 0 && u.cacheReadTokens >= 0 && u.cacheWriteTokens >= 0 && u.totalTokens >= 0 && u.totalTokens == u.inputTokens+u.outputTokens && u.reasoningTokens <= u.outputTokens && u.cacheReadTokens <= u.inputTokens && u.cacheWriteTokens <= u.inputTokens
}

// AtLeast reports whether every usage counter is monotonic relative to previous.
func (u Usage) AtLeast(previous Usage) bool {
	return u.Valid() && previous.Valid() && u.inputTokens >= previous.inputTokens && u.outputTokens >= previous.outputTokens && u.reasoningTokens >= previous.reasoningTokens && u.cacheReadTokens >= previous.cacheReadTokens && u.cacheWriteTokens >= previous.cacheWriteTokens && u.totalTokens >= previous.totalTokens
}
