package canonical

// Role identifies a canonical message author role.
type Role string

// ToolChoiceKind controls canonical tool selection.
type ToolChoiceKind string

// OutputFormat identifies the requested response representation.
type OutputFormat string

// ImageDetail is a provider-neutral image quality hint.
type ImageDetail string

// ItemType discriminates canonical item variants.
type ItemType string

// ContentType discriminates canonical content variants.
type ContentType string

// EventType discriminates canonical stream event variants.
type EventType string

// FinishReason explains why canonical generation terminated.
type FinishReason string

// FailureCode identifies a normalized canonical failure category.
type FailureCode string

// Valid reports whether the role is supported by protocol version 1.
func (v Role) Valid() bool { return oneOf(string(v), "system", "developer", "user", "assistant") }

// Valid reports whether the tool-choice kind is supported.
func (v ToolChoiceKind) Valid() bool { return oneOf(string(v), "auto", "none", "required", "specific") }

// Valid reports whether the output format is supported.
func (v OutputFormat) Valid() bool { return oneOf(string(v), "text", "json_object", "json_schema") }

// Valid reports whether the image detail hint is supported.
func (v ImageDetail) Valid() bool { return oneOf(string(v), "auto", "low", "high") }

// Valid reports whether the item type is supported.
func (v ItemType) Valid() bool {
	return oneOf(string(v), "message", "tool_call", "tool_result", "reasoning_summary")
}

// Valid reports whether the content type is supported.
func (v ContentType) Valid() bool { return oneOf(string(v), "text", "image", "json") }

// Valid reports whether the event type is supported.
func (v EventType) Valid() bool {
	return oneOf(string(v), "response_started", "item_started", "content_started", "text_delta", "json_delta", "tool_arguments_delta", "content_completed", "item_completed", "usage_updated", "response_completed", "response_failed", "response_cancelled")
}

// Valid reports whether the finish reason is supported.
func (v FinishReason) Valid() bool {
	return oneOf(string(v), "stop", "length", "tool_call", "content_filter", "cancelled", "error", "other")
}

// Valid reports whether the normalized failure code is supported.
func (v FailureCode) Valid() bool {
	return oneOf(string(v), "invalid_request", "authentication_failed", "permission_denied", "model_unavailable", "rate_limited", "quota_exhausted", "upstream_timeout", "upstream_connection_failed", "upstream_protocol_error", "stream_interrupted", "router_overloaded", "cancelled", "internal_error")
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
