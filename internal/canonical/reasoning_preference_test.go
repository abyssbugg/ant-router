package canonical

import (
	"bytes"
	"testing"
)

func TestReasoningSummaryModeValues(t *testing.T) {
	for _, mode := range []ReasoningSummaryMode{ReasoningSummaryNone, ReasoningSummaryAuto, ReasoningSummaryConcise, ReasoningSummaryDetailed} {
		if !mode.Valid() {
			t.Errorf("mode %q is invalid", mode)
		}
	}
	if ReasoningSummaryMode("unknown").Valid() {
		t.Fatal("unknown reasoning summary mode accepted")
	}
}

func TestRequestReasoningSummaryPreference(t *testing.T) {
	base := codecRequest(t, 1)
	if base.ReasoningSummary() != ReasoningSummaryNone || base.Capabilities().Has(CapabilityReasoningSummary) {
		t.Fatalf("default reasoning summary = %q, capabilities = %v", base.ReasoningSummary(), base.Capabilities().Values())
	}
	detailed, err := requestWithReasoningSummary(base, ReasoningSummaryDetailed)
	if err != nil {
		t.Fatal(err)
	}
	if detailed.ReasoningSummary() != ReasoningSummaryDetailed || !detailed.Capabilities().Has(CapabilityReasoningSummary) {
		t.Fatalf("detailed reasoning summary = %q, capabilities = %v", detailed.ReasoningSummary(), detailed.Capabilities().Values())
	}
	if _, err := requestWithReasoningSummary(base, ReasoningSummaryMode("unknown")); err == nil {
		t.Fatal("unknown reasoning summary mode accepted")
	}
}

func TestRequestCodecPreservesReasoningSummaryPreference(t *testing.T) {
	base := codecRequest(t, 1)
	concise, err := requestWithReasoningSummary(base, ReasoningSummaryConcise)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeRequest(concise)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"reasoning_summary":"concise"`)) {
		t.Fatalf("reasoning summary missing: %s", encoded)
	}
	decoded, err := DecodeRequest(encoded)
	if err != nil || decoded.ReasoningSummary() != ReasoningSummaryConcise {
		t.Fatalf("decoded reasoning summary = %q, %v", decoded.ReasoningSummary(), err)
	}
	baseDigest, _ := FullDigest(base)
	conciseDigest, _ := FullDigest(concise)
	if baseDigest == conciseDigest {
		t.Fatal("reasoning summary did not affect request digest")
	}
}

func TestRequestCodecDefaultsMissingReasoningSummary(t *testing.T) {
	encoded, err := EncodeRequest(codecRequest(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	legacy := bytes.Replace(encoded, []byte(`,"reasoning_summary":"none"`), nil, 1)
	if bytes.Equal(legacy, encoded) {
		t.Fatal("test fixture did not remove reasoning summary")
	}
	decoded, err := DecodeRequest(legacy)
	if err != nil || decoded.ReasoningSummary() != ReasoningSummaryNone {
		t.Fatalf("legacy reasoning summary = %q, %v", decoded.ReasoningSummary(), err)
	}
}

func requestWithReasoningSummary(base Request, mode ReasoningSummaryMode) (Request, error) {
	return NewRequest(RequestParams{
		ID: base.ID(), Model: base.Model(), Input: base.Input(), Tools: base.Tools(),
		ToolChoice: base.ToolChoice(), Output: base.Output(), Sampling: base.Sampling(),
		Continuation: base.Continuation(), ReasoningSummary: mode,
	})
}
