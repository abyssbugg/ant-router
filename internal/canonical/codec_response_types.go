package canonical

import "encoding/json"

type responseDTO struct {
	Version      string            `json:"version"`
	ID           string            `json:"id"`
	Model        string            `json:"model"`
	Items        []json.RawMessage `json:"items"`
	FinishReason string            `json:"finish_reason"`
	Usage        usageDTO          `json:"usage"`
	Continuation *continuationDTO  `json:"continuation,omitempty"`
}

type usageDTO struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type failureDTO struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
	RetryAfterNS *int64 `json:"retry_after_ns,omitempty"`
}

type eventDTO struct {
	Version    string          `json:"version"`
	ID         string          `json:"id"`
	ResponseID string          `json:"response_id"`
	Sequence   uint64          `json:"sequence"`
	Type       string          `json:"type"`
	Data       json.RawMessage `json:"data"`
}

type responseStartedDTO struct {
	Model string `json:"model"`
}
type continuationUpdatedDTO struct {
	Continuation continuationDTO `json:"continuation"`
}
type itemStartedDTO struct {
	Index     int    `json:"index"`
	ID        string `json:"id"`
	ItemType  string `json:"item_type"`
	Role      string `json:"role,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}
type contentStartedDTO struct {
	ItemID       string `json:"item_id"`
	ContentIndex int    `json:"content_index"`
	ContentType  string `json:"content_type"`
}
type deltaDTO struct {
	ItemID       string `json:"item_id"`
	ContentIndex int    `json:"content_index"`
	Delta        string `json:"delta"`
}
type toolArgumentsDeltaDTO struct {
	ItemID string `json:"item_id"`
	Delta  string `json:"delta"`
}
type contentCompletedDTO struct {
	ItemID       string          `json:"item_id"`
	ContentIndex int             `json:"content_index"`
	Content      json.RawMessage `json:"content"`
}
type itemCompletedDTO struct {
	Index int             `json:"index"`
	Item  json.RawMessage `json:"item"`
}
type usageUpdatedDTO struct {
	Usage usageDTO `json:"usage"`
}
type responseCompletedDTO struct {
	FinishReason string   `json:"finish_reason"`
	Usage        usageDTO `json:"usage"`
	ItemIDs      []string `json:"item_ids"`
}
type responseFailedDTO struct {
	Failure failureDTO `json:"failure"`
	Usage   *usageDTO  `json:"usage,omitempty"`
}
