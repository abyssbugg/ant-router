package canonical

import "testing"

func TestCanonicalEnums(t *testing.T) {
	tests := []struct {
		name    string
		valid   []string
		invalid []string
		check   func(string) bool
	}{
		{"role", []string{"system", "developer", "user", "assistant"}, []string{"", "tool", "User"}, func(v string) bool { return Role(v).Valid() }},
		{"tool choice", []string{"auto", "none", "required", "specific"}, []string{"", "any"}, func(v string) bool { return ToolChoiceKind(v).Valid() }},
		{"output format", []string{"text", "json_object", "json_schema"}, []string{"", "json"}, func(v string) bool { return OutputFormat(v).Valid() }},
		{"image detail", []string{"auto", "low", "high"}, []string{"", "medium"}, func(v string) bool { return ImageDetail(v).Valid() }},
		{"item type", []string{"message", "tool_call", "tool_result", "reasoning_summary"}, []string{"", "image"}, func(v string) bool { return ItemType(v).Valid() }},
		{"content type", []string{"text", "image", "json"}, []string{"", "audio"}, func(v string) bool { return ContentType(v).Valid() }},
		{"finish reason", []string{"stop", "length", "tool_call", "content_filter", "cancelled", "error", "other"}, []string{"", "done"}, func(v string) bool { return FinishReason(v).Valid() }},
		{"failure", []string{"invalid_request", "authentication_failed", "permission_denied", "model_unavailable", "rate_limited", "quota_exhausted", "upstream_timeout", "upstream_connection_failed", "upstream_protocol_error", "stream_interrupted", "router_overloaded", "cancelled", "internal_error"}, []string{"", "unknown"}, func(v string) bool { return FailureCode(v).Valid() }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, value := range test.valid {
				if !test.check(value) {
					t.Errorf("%q rejected", value)
				}
			}
			for _, value := range test.invalid {
				if test.check(value) {
					t.Errorf("%q accepted", value)
				}
			}
		})
	}
}

func TestEventTypes(t *testing.T) {
	valid := []string{"response_started", "item_started", "content_started", "text_delta", "json_delta", "tool_arguments_delta", "content_completed", "item_completed", "usage_updated", "response_completed", "response_failed", "response_cancelled"}
	for _, value := range valid {
		if !EventType(value).Valid() {
			t.Errorf("EventType(%q) rejected", value)
		}
	}
	if EventType("").Valid() || EventType("delta").Valid() {
		t.Error("invalid event type accepted")
	}
}
