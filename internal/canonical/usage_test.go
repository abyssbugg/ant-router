package canonical

import "testing"

func TestUsageAndMonotonicity(t *testing.T) {
	usage, err := NewUsage(UsageParams{InputTokens: 100, OutputTokens: 50, ReasoningTokens: 20, CacheReadTokens: 40, CacheWriteTokens: 10, TotalTokens: 150})
	if err != nil || !usage.Valid() || usage.InputTokens() != 100 || usage.TotalTokens() != 150 {
		t.Fatalf("NewUsage() = %#v, %v", usage, err)
	}
	later, _ := NewUsage(UsageParams{InputTokens: 110, OutputTokens: 60, ReasoningTokens: 25, CacheReadTokens: 40, CacheWriteTokens: 15, TotalTokens: 170})
	if !later.AtLeast(usage) || usage.AtLeast(later) {
		t.Error("usage monotonic comparison is incorrect")
	}
}

func TestUsageRejectsInvalidArithmetic(t *testing.T) {
	tests := []UsageParams{
		{InputTokens: -1},
		{OutputTokens: -1},
		{InputTokens: 1, OutputTokens: 1, TotalTokens: 1},
		{OutputTokens: 1, ReasoningTokens: 2, TotalTokens: 1},
		{InputTokens: 1, CacheReadTokens: 2, TotalTokens: 1},
		{InputTokens: 1, CacheWriteTokens: 2, TotalTokens: 1},
	}
	for _, params := range tests {
		if _, err := NewUsage(params); codeOfCanonical(err) != CodeInvalidInvariant {
			t.Errorf("params %#v code = %q", params, codeOfCanonical(err))
		}
	}
	zero, err := NewUsage(UsageParams{})
	if err != nil || !zero.Valid() {
		t.Errorf("zero usage = %#v, %v", zero, err)
	}
}
