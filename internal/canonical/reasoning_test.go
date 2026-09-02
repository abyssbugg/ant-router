package canonical

import "testing"

func TestReasoningSummary(t *testing.T) {
	first, _ := NewTextContent("first")
	second, _ := NewTextContent("second")
	blocks := []TextContent{first, second}
	summary, err := NewReasoningSummary(testItemID(t, 4), blocks)
	if err != nil || !summary.Valid() || len(summary.Summaries()) != 2 {
		t.Fatalf("NewReasoningSummary() = %#v, %v", summary, err)
	}
	blocks[0] = TextContent{}
	copyValue := summary.Summaries()
	copyValue[0] = TextContent{}
	if !summary.Summaries()[0].Valid() {
		t.Error("reasoning summary was mutated")
	}
}

func TestReasoningSummaryRejectsInvalid(t *testing.T) {
	valid, _ := NewTextContent("summary")
	if _, err := NewReasoningSummary("", []TextContent{valid}); codeOfCanonical(err) != CodeInvalidID {
		t.Error("invalid summary ID accepted")
	}
	if _, err := NewReasoningSummary(testItemID(t, 1), []TextContent{{}}); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Error("invalid text accepted")
	}
}
