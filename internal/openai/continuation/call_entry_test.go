package continuation

import (
	"bytes"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func continuationCallID(t *testing.T, value byte) canonical.ToolCallID {
	t.Helper()
	id, err := canonical.GenerateToolCallID(bytes.NewReader(bytes.Repeat([]byte{value}, 16)))
	if err != nil {
		t.Fatalf("GenerateToolCallID() failed: %v", err)
	}
	return id
}

func TestContinuationEnvelopeRoundTripsFunctionCallIdentity(t *testing.T) {
	reasoning, err := NewContinuationEntry(continuationItemID(t, 1), "rs_upstream", "cipher-marker")
	if err != nil {
		t.Fatal(err)
	}
	call, err := NewFunctionCallEntry(continuationItemID(t, 2), continuationCallID(t, 3), "fc_upstream", "call_upstream")
	if err != nil {
		t.Fatal(err)
	}
	value, err := EncodeContinuation("openai-primary", "gpt-5.2", []ContinuationEntry{reasoning, call})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeContinuation(value, "openai-primary", "gpt-5.2", []canonical.ItemID{reasoning.CanonicalItemID(), call.CanonicalItemID()})
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || !decoded[0].IsReasoning() || !decoded[1].IsFunctionCall() {
		t.Fatalf("decoded entry kinds are invalid: %#v", decoded)
	}
	if decoded[1].CanonicalCallID() != call.CanonicalCallID() || decoded[1].UpstreamItemID() != "fc_upstream" || decoded[1].UpstreamCallID() != "call_upstream" || decoded[1].EncryptedContent() != "" {
		t.Fatal("function-call identity did not round trip")
	}
}

func TestContinuationEnvelopeRejectsDuplicateFunctionCallIdentity(t *testing.T) {
	first, err := NewFunctionCallEntry(continuationItemID(t, 1), continuationCallID(t, 3), "fc_first", "call_first")
	if err != nil {
		t.Fatal(err)
	}
	for name, second := range map[string]ContinuationEntry{
		"canonical_call": mustFunctionCallEntry(t, continuationItemID(t, 2), continuationCallID(t, 3), "fc_second", "call_second"),
		"upstream_call":  mustFunctionCallEntry(t, continuationItemID(t, 2), continuationCallID(t, 4), "fc_second", "call_first"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := EncodeContinuation("openai-primary", "gpt-5.2", []ContinuationEntry{first, second}); err == nil {
				t.Fatal("duplicate function-call identity was accepted")
			}
		})
	}
}

func TestContinuationEnvelopeRejectsCrossKindFieldsWithoutLeak(t *testing.T) {
	marker := "private-call-marker"
	item := continuationItemID(t, 1).String()
	call := continuationCallID(t, 2).String()
	for name, document := range map[string]string{
		"reasoning_call_fields": `{"entries":[{"canonical_call_id":"` + call + `","canonical_item_id":"` + item + `","encrypted_content":"cipher","upstream_call_id":"` + marker + `","upstream_item_id":"rs_one"}],"version":1}`,
		"call_encrypted":        `{"entries":[{"canonical_call_id":"` + call + `","canonical_item_id":"` + item + `","encrypted_content":"` + marker + `","kind":"function_call","upstream_call_id":"call_one","upstream_item_id":"fc_one"}],"version":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			value, err := canonical.NewContinuation("openai-primary", "gpt-5.2", []byte(document))
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeContinuation(value, "openai-primary", "gpt-5.2", nil)
			if err == nil || strings.Contains(err.Error(), marker) {
				t.Fatalf("cross-kind error = %v", err)
			}
		})
	}
}

func mustFunctionCallEntry(t *testing.T, itemID canonical.ItemID, callID canonical.ToolCallID, upstreamItemID, upstreamCallID string) ContinuationEntry {
	t.Helper()
	entry, err := NewFunctionCallEntry(itemID, callID, upstreamItemID, upstreamCallID)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}
