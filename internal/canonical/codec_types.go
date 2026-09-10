package canonical

import (
	"encoding/json"
)

type requestDTO struct {
	Version          string            `json:"version"`
	ID               string            `json:"id"`
	Model            string            `json:"model"`
	Input            []json.RawMessage `json:"input"`
	Tools            []toolDTO         `json:"tools"`
	ToolChoice       toolChoiceDTO     `json:"tool_choice"`
	Output           outputDTO         `json:"output"`
	Sampling         samplingDTO       `json:"sampling"`
	ReasoningSummary string            `json:"reasoning_summary"`
	Continuation     *continuationDTO  `json:"continuation,omitempty"`
}

type toolDTO struct {
	Type               string               `json:"type,omitempty"`
	Name               string               `json:"name"`
	Description        string               `json:"description"`
	InputSchema        json.RawMessage      `json:"input_schema,omitempty"`
	SchemaEnforcement  string               `json:"schema_enforcement,omitempty"`
	Tools              []toolDTO            `json:"tools,omitempty"`
	CustomFormat       *customToolFormatDTO `json:"custom_format,omitempty"`
	ExternalWebAccess  *bool                `json:"external_web_access,omitempty"`
	IndexedWebAccess   *bool                `json:"indexed_web_access,omitempty"`
	Filters            json.RawMessage      `json:"filters,omitempty"`
	UserLocation       json.RawMessage      `json:"user_location,omitempty"`
	SearchContextSize  string               `json:"search_context_size,omitempty"`
	SearchContentTypes []string             `json:"search_content_types,omitempty"`
}

type customToolFormatDTO struct {
	Type       string `json:"type"`
	Syntax     string `json:"syntax,omitempty"`
	Definition string `json:"definition,omitempty"`
}

type toolChoiceDTO struct {
	Type      string `json:"type"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

type outputDTO struct {
	Format            string          `json:"format"`
	SchemaEnforcement string          `json:"schema_enforcement"`
	SchemaName        string          `json:"schema_name,omitempty"`
	Schema            json.RawMessage `json:"schema,omitempty"`
	MaxTokens         int             `json:"max_tokens,omitempty"`
	ParallelToolCalls bool            `json:"parallel_tool_calls"`
}

type samplingDTO struct {
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	Seed        *int64   `json:"seed,omitempty"`
}

type continuationDTO struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Data     []byte `json:"data"`
}

type messageDTO struct {
	Type    string            `json:"type"`
	ID      string            `json:"id"`
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type toolCallDTO struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	CallID    string          `json:"call_id"`
	Namespace string          `json:"namespace,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type customToolCallDTO struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Input     string `json:"input"`
}

type hostedSearchCallDTO struct {
	Type   string                `json:"type"`
	ID     string                `json:"id"`
	Status string                `json:"status"`
	Action hostedSearchActionDTO `json:"action"`
}

type hostedSearchActionDTO struct {
	Type    string   `json:"type"`
	Queries []string `json:"queries,omitempty"`
	Sources []string `json:"sources,omitempty"`
	URL     string   `json:"url,omitempty"`
	Pattern string   `json:"pattern,omitempty"`
}

type toolResultDTO struct {
	Type    string            `json:"type"`
	ID      string            `json:"id"`
	CallID  string            `json:"call_id"`
	Content []json.RawMessage `json:"content"`
	IsError bool              `json:"is_error"`
}

type reasoningDTO struct {
	Type      string   `json:"type"`
	ID        string   `json:"id"`
	Summaries []string `json:"summaries"`
}

type textContentDTO struct {
	Type      string           `json:"type"`
	Text      string           `json:"text"`
	Citations []urlCitationDTO `json:"citations,omitempty"`
}

type urlCitationDTO struct {
	StartIndex int    `json:"start_index"`
	EndIndex   int    `json:"end_index"`
	Title      string `json:"title"`
	URL        string `json:"url"`
}

type imageContentDTO struct {
	Type     string         `json:"type"`
	Source   imageSourceDTO `json:"source"`
	MIMEType string         `json:"mime_type"`
	Detail   string         `json:"detail"`
	Width    int            `json:"width,omitempty"`
	Height   int            `json:"height,omitempty"`
}

type imageSourceDTO struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
	Data []byte `json:"data,omitempty"`
}

type jsonContentDTO struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}
