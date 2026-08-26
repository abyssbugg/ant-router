package canonical

import (
	"encoding/json"
)

type requestDTO struct {
	Version      string            `json:"version"`
	ID           string            `json:"id"`
	Model        string            `json:"model"`
	Input        []json.RawMessage `json:"input"`
	Tools        []toolDTO         `json:"tools"`
	ToolChoice   toolChoiceDTO     `json:"tool_choice"`
	Output       outputDTO         `json:"output"`
	Sampling     samplingDTO       `json:"sampling"`
	Continuation *continuationDTO  `json:"continuation,omitempty"`
}

type toolDTO struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type toolChoiceDTO struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type outputDTO struct {
	Format            string          `json:"format"`
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
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
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
	Type string `json:"type"`
	Text string `json:"text"`
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
