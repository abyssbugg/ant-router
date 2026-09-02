package continuation

import (
	"bytes"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func continuationItemID(t *testing.T, value byte) canonical.ItemID {
	t.Helper()
	id, err := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{value}, 16)))
	if err != nil {
		t.Fatalf("GenerateItemID() failed: %v", err)
	}
	return id
}

func TestContinuationEnvelopeRoundTripIsDeterministicAndDefensive(t *testing.T) {
	entries := []continuationEntry{
		{canonicalItemID: continuationItemID(t, 1), upstreamItemID: "rs_upstream_1", encryptedContent: "cipher-one"},
		{canonicalItemID: continuationItemID(t, 2), upstreamItemID: "rs_upstream_2", encryptedContent: "cipher-two"},
	}
	continuation, err := encodeContinuationEnvelope("openai-primary", "gpt-5.2", entries)
	if err != nil {
		t.Fatalf("encodeContinuationEnvelope() failed: %v", err)
	}
	first := continuation.Bytes()
	second := continuation.Bytes()
	if !bytes.Equal(first, second) || string(first) != `{"entries":[{"canonical_item_id":"item_AQEBAQEBAQEBAQEBAQEBAQ","encrypted_content":"cipher-one","upstream_item_id":"rs_upstream_1"},{"canonical_item_id":"item_AgICAgICAgICAgICAgICAg","encrypted_content":"cipher-two","upstream_item_id":"rs_upstream_2"}],"version":1}` {
		t.Fatalf("unexpected deterministic envelope: %s", first)
	}
	first[0] = 'x'
	if bytes.Equal(first, continuation.Bytes()) {
		t.Fatal("continuation bytes were aliased")
	}
	decoded, err := decodeContinuationEnvelope(continuation, "openai-primary", "gpt-5.2", []canonical.ItemID{entries[0].canonicalItemID, entries[1].canonicalItemID})
	if err != nil {
		t.Fatalf("decodeContinuationEnvelope() failed: %v", err)
	}
	got := decoded.entriesCopy()
	if len(got) != 2 || got[0] != entries[0] || got[1] != entries[1] {
		t.Fatalf("decoded entries=%#v", got)
	}
	got[0].encryptedContent = "changed"
	if decoded.entriesCopy()[0].encryptedContent != "cipher-one" {
		t.Fatal("decoded entries were aliased")
	}
}

func TestContinuationEnvelopeAppendIsCumulative(t *testing.T) {
	first := continuationEntry{canonicalItemID: continuationItemID(t, 1), upstreamItemID: "rs_1", encryptedContent: "cipher-1"}
	envelope, err := newContinuationEnvelope([]continuationEntry{first})
	if err != nil {
		t.Fatal(err)
	}
	second := continuationEntry{canonicalItemID: continuationItemID(t, 2), upstreamItemID: "rs_2", encryptedContent: "cipher-2"}
	updated, err := envelope.append(second)
	if err != nil {
		t.Fatalf("append() failed: %v", err)
	}
	if len(envelope.entriesCopy()) != 1 || len(updated.entriesCopy()) != 2 {
		t.Fatalf("original=%#v updated=%#v", envelope.entriesCopy(), updated.entriesCopy())
	}
}

func TestContinuationEnvelopeRejectsAffinityAndDanglingAnchors(t *testing.T) {
	entry := continuationEntry{canonicalItemID: continuationItemID(t, 1), upstreamItemID: "rs_1", encryptedContent: "secret-marker"}
	continuation, err := encodeContinuationEnvelope("openai-primary", "gpt-5.2", []continuationEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	for name, testCase := range map[string]struct{ provider, model string }{
		"provider": {provider: "openai-secondary", model: "gpt-5.2"},
		"model":    {provider: "openai-primary", model: "gpt-5.3"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeContinuationEnvelope(continuation, testCase.provider, testCase.model, []canonical.ItemID{entry.canonicalItemID})
			if err == nil || strings.Contains(err.Error(), "secret-marker") {
				t.Fatalf("affinity error=%v", err)
			}
		})
	}
	_, err = decodeContinuationEnvelope(continuation, "openai-primary", "gpt-5.2", []canonical.ItemID{continuationItemID(t, 9)})
	if err == nil || strings.Contains(err.Error(), "secret-marker") {
		t.Fatalf("dangling anchor error=%v", err)
	}
}

func TestContinuationEnvelopeRejectsMalformedDocumentsWithoutLeakingValues(t *testing.T) {
	marker := "secret-marker"
	item := continuationItemID(t, 1).String()
	cases := map[string]string{
		"version":            `{"entries":[],"version":2}`,
		"unknown":            `{"entries":[],"extra":"secret-marker","version":1}`,
		"duplicate_key":      `{"entries":[],"entries":[],"version":1}`,
		"empty":              `{"entries":[],"version":1}`,
		"duplicate_item":     `{"entries":[{"canonical_item_id":"` + item + `","encrypted_content":"a","upstream_item_id":"rs_1"},{"canonical_item_id":"` + item + `","encrypted_content":"b","upstream_item_id":"rs_2"}],"version":1}`,
		"duplicate_upstream": `{"entries":[{"canonical_item_id":"` + item + `","encrypted_content":"a","upstream_item_id":"rs_1"},{"canonical_item_id":"` + continuationItemID(t, 2).String() + `","encrypted_content":"b","upstream_item_id":"rs_1"}],"version":1}`,
		"control":            `{"entries":[{"canonical_item_id":"` + item + `","encrypted_content":"secret-marker\u0001","upstream_item_id":"rs_1"}],"version":1}`,
		"bad_upstream":       `{"entries":[{"canonical_item_id":"` + item + `","encrypted_content":"a","upstream_item_id":"secret marker"}],"version":1}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			continuation, err := canonical.NewContinuation("openai-primary", "gpt-5.2", []byte(document))
			if err != nil {
				t.Fatal(err)
			}
			_, err = decodeContinuationEnvelope(continuation, "openai-primary", "gpt-5.2", nil)
			if err == nil || strings.Contains(err.Error(), marker) {
				t.Fatalf("malformed envelope error=%v", err)
			}
		})
	}
}

func TestContinuationEnvelopeRejectsInvalidEntriesAndEncodedOverflow(t *testing.T) {
	id := continuationItemID(t, 1)
	for name, entry := range map[string]continuationEntry{
		"zero_item":      {},
		"empty_upstream": {canonicalItemID: id, encryptedContent: "cipher"},
		"empty_content":  {canonicalItemID: id, upstreamItemID: "rs_1"},
		"control":        {canonicalItemID: id, upstreamItemID: "rs_1", encryptedContent: "cipher\nvalue"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newContinuationEnvelope([]continuationEntry{entry})
			if err == nil || (entry.encryptedContent != "" && strings.Contains(err.Error(), entry.encryptedContent)) {
				t.Fatalf("entry error=%v", err)
			}
		})
	}
	overflow := continuationEntry{canonicalItemID: id, upstreamItemID: "rs_1", encryptedContent: strings.Repeat("x", canonical.MaxContinuationBytes)}
	if _, err := newContinuationEnvelope([]continuationEntry{overflow}); err == nil {
		t.Fatal("oversized envelope construction accepted")
	}
	if _, err := encodeContinuationEnvelope("openai-primary", "gpt-5.2", []continuationEntry{overflow}); err == nil {
		t.Fatal("oversized encoded envelope accepted")
	}
}

func TestContinuationEnvelopeRejectsDuplicateAppend(t *testing.T) {
	entry := continuationEntry{canonicalItemID: continuationItemID(t, 1), upstreamItemID: "rs_1", encryptedContent: "cipher"}
	envelope, err := newContinuationEnvelope([]continuationEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := envelope.append(entry); err == nil {
		t.Fatal("duplicate append accepted")
	}
}
