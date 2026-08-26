package canonical

import "time"

func encodeUsage(value Usage) usageDTO {
	return usageDTO{InputTokens: value.InputTokens(), OutputTokens: value.OutputTokens(), ReasoningTokens: value.ReasoningTokens(), CacheReadTokens: value.CacheReadTokens(), CacheWriteTokens: value.CacheWriteTokens(), TotalTokens: value.TotalTokens()}
}

func decodeUsage(dto usageDTO) (Usage, error) {
	return NewUsage(UsageParams{InputTokens: dto.InputTokens, OutputTokens: dto.OutputTokens, ReasoningTokens: dto.ReasoningTokens, CacheReadTokens: dto.CacheReadTokens, CacheWriteTokens: dto.CacheWriteTokens, TotalTokens: dto.TotalTokens})
}

func encodeFailure(value Failure) failureDTO {
	dto := failureDTO{Code: string(value.Code()), Message: value.Message(), Retryable: value.Retryable()}
	if retryAfter, ok := value.RetryAfter(); ok {
		nanoseconds := int64(retryAfter)
		dto.RetryAfterNS = &nanoseconds
	}
	return dto
}

func decodeFailure(dto failureDTO) (Failure, error) {
	params := FailureParams{Code: FailureCode(dto.Code), Message: dto.Message, Retryable: dto.Retryable}
	if dto.RetryAfterNS != nil {
		retryAfter := time.Duration(*dto.RetryAfterNS)
		params.RetryAfter = &retryAfter
	}
	return NewFailure(params)
}
