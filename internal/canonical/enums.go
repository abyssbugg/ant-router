package canonical

// Role identifies a canonical message author role.
type Role string

// ToolChoiceKind controls canonical tool selection.
type ToolChoiceKind string

// OutputFormat identifies the requested response representation.
type OutputFormat string

// SchemaEnforcement identifies how strictly a provider must enforce a JSON schema.
type SchemaEnforcement string

// ReasoningSummaryMode identifies the requested reasoning-summary detail.
type ReasoningSummaryMode string

// ImageDetail is a provider-neutral image quality hint.
type ImageDetail string

const (
	SchemaEnforcementBestEffort SchemaEnforcement = "best_effort"
	SchemaEnforcementStrict     SchemaEnforcement = "strict"

	ReasoningSummaryNone     ReasoningSummaryMode = "none"
	ReasoningSummaryAuto     ReasoningSummaryMode = "auto"
	ReasoningSummaryConcise  ReasoningSummaryMode = "concise"
	ReasoningSummaryDetailed ReasoningSummaryMode = "detailed"

	ImageDetailAuto ImageDetail = "auto"
	ImageDetailLow  ImageDetail = "low"
	ImageDetailHigh ImageDetail = "high"
)

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

// Valid reports whether the schema enforcement mode is supported.
func (v SchemaEnforcement) Valid() bool { return oneOf(string(v), "best_effort", "strict") }

// Valid reports whether the reasoning summary mode is supported.
func (v ReasoningSummaryMode) Valid() bool {
	return oneOf(string(v), "none", "auto", "concise", "detailed")
}

// Valid reports whether the image detail hint is supported.
func (v ImageDetail) Valid() bool { return oneOf(string(v), "auto", "low", "high") }

// Valid reports whether the item type is supported.
func (v ItemType) Valid() bool {
	return oneOf(string(v), "message", "tool_call", "custom_tool_call", "tool_result", "reasoning_summary")
}

// Valid reports whether the content type is supported.
func (v ContentType) Valid() bool { return oneOf(string(v), "text", "image", "json") }

// Valid reports whether the event type is supported.
func (v EventType) Valid() bool {
	return oneOf(string(v), "response_started", "continuation_updated", "item_started", "content_started", "text_delta", "json_delta", "tool_arguments_delta", "custom_input_delta", "custom_input_completed", "content_completed", "item_completed", "usage_updated", "response_completed", "response_failed", "response_cancelled")
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
