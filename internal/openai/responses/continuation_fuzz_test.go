package responses

import (
	"bytes"
	"strings"
	"testing"
)

func FuzzDecodeContinuationReplay(f *testing.F) {
	f.Add([]byte(`{"model":"openai/public","input":"hello"}`))
	f.Add([]byte(`{"model":"openai/public","store":false,"include":["reasoning.encrypted_content"],"input":[{"type":"reasoning","id":"rs_upstream","encrypted_content":"cipher-replay","summary":[]},{"type":"function_call","id":"fc_upstream","call_id":"call_upstream","name":"Search","arguments":"{}","status":"completed"},{"type":"function_call_output","call_id":"call_upstream","output":"done"}],"tools":[{"type":"function","name":"Search","parameters":{"type":"object"},"strict":true}]}`))
	f.Add([]byte(`{"model":"openai/public","store":true,"input":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxRequestBodyBytes {
			t.Skip()
		}
		result, err := DecodeWithAffinity(bytes.NewReader(data), &sequenceIDs{}, &fixedAffinityResolver{providerID: "openai-primary", upstreamModel: "gpt-test"})
		if err != nil {
			message := err.Error()
			for _, marker := range []string{"cipher-replay", "rs_upstream", "fc_upstream", "call_upstream"} {
				if strings.Contains(message, marker) {
					t.Fatalf("error leaked protected marker")
				}
			}
			return
		}
		if !result.Request.Valid() {
			t.Fatalf("successful decode produced invalid request")
		}
	})
}
