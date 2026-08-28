package responses

import (
	"bytes"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

type sequenceIDs struct{ next byte }

func (s *sequenceIDs) material() *bytes.Reader {
	s.next++
	return bytes.NewReader(bytes.Repeat([]byte{s.next}, canonical.IDRandomBytes))
}
func (s *sequenceIDs) RequestID() (canonical.RequestID, error) {
	return canonical.GenerateRequestID(s.material())
}
func (s *sequenceIDs) ItemID() (canonical.ItemID, error) {
	return canonical.GenerateItemID(s.material())
}
func (s *sequenceIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(s.material())
}

func TestDecodeRejectsTypedNilIDGenerator(t *testing.T) {
	var ids *sequenceIDs
	_, err := Decode(strings.NewReader(`{"model":"mock/text","input":"hello"}`), ids)
	if CodeOf(err) != CodeInternal || PathOf(err) != "request" {
		t.Fatalf("typed-nil IDs error = %v", err)
	}
}

func TestDecodeStringInputToCanonicalRequest(t *testing.T) {
	parsed, err := Decode(strings.NewReader(`{
		"model":"mock/text",
		"instructions":"Follow instructions",
		"input":"Hello",
		"temperature":0.5,
		"top_p":0.8,
		"max_output_tokens":128,
		"parallel_tool_calls":false,
		"stream":true
	}`), &sequenceIDs{})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !parsed.Stream || parsed.Request.Model() != "mock/text" {
		t.Fatalf("parsed request = stream:%v model:%q", parsed.Stream, parsed.Request.Model())
	}
	items := parsed.Request.Input()
	if len(items) != 2 {
		t.Fatalf("input items = %d, want 2", len(items))
	}
	want := []struct {
		role canonical.Role
		text string
	}{{"system", "Follow instructions"}, {"user", "Hello"}}
	for index, expected := range want {
		message, ok := items[index].Message()
		if !ok || message.Role() != expected.role {
			t.Fatalf("item %d role = %q", index, message.Role())
		}
		blocks := message.Content()
		text, ok := blocks[0].Text()
		if !ok || text.Text() != expected.text {
			t.Fatalf("item %d text mismatch", index)
		}
	}
	if temperature, ok := parsed.Request.Sampling().Temperature(); !ok || temperature != 0.5 {
		t.Fatalf("temperature = %v, %v", temperature, ok)
	}
	if topP, ok := parsed.Request.Sampling().TopP(); !ok || topP != 0.8 {
		t.Fatalf("top_p = %v, %v", topP, ok)
	}
	if parsed.Request.Output().MaxTokens() != 128 || parsed.Request.Output().ParallelToolCalls() {
		t.Fatalf("output constraints = max:%d parallel:%v", parsed.Request.Output().MaxTokens(), parsed.Request.Output().ParallelToolCalls())
	}
}

func TestDecodeDefaultsOptionalControls(t *testing.T) {
	parsed, err := Decode(strings.NewReader(`{"model":"mock/text","input":"Hello"}`), &sequenceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Stream || parsed.Request.Output().Format() != canonical.OutputFormat("text") || parsed.Request.ToolChoice().Kind() != canonical.ToolChoiceKind("auto") {
		t.Fatalf("defaults were not applied")
	}
}

func TestDecodeRejectsStrictTopLevelViolations(t *testing.T) {
	tests := []struct {
		name string
		body string
		code ErrorCode
		path string
	}{
		{"unknown", `{"model":"mock/text","input":"hi","typo":true}`, CodeUnknownField, "typo"},
		{"unsupported", `{"model":"mock/text","input":"hi","previous_response_id":"secret"}`, CodeUnsupportedField, "previous_response_id"},
		{"trailing", `{"model":"mock/text","input":"hi"} {}`, CodeInvalidJSON, "request"},
		{"missing model", `{"input":"hi"}`, CodeInvalidField, "model"},
		{"empty input", `{"model":"mock/text","input":""}`, CodeInvalidField, "input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(tt.body), &sequenceIDs{})
			if CodeOf(err) != tt.code || PathOf(err) != tt.path {
				t.Fatalf("Decode() = code:%q path:%q, want %q/%q", CodeOf(err), PathOf(err), tt.code, tt.path)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("error exposed request value")
			}
		})
	}
}

func TestDecodeReportsUnsupportedFieldsDeterministically(t *testing.T) {
	body := `{"model":"mock/text","input":"hi","store":true,"previous_response_id":"id","background":true}`
	for range 100 {
		_, err := Decode(strings.NewReader(body), &sequenceIDs{})
		if CodeOf(err) != CodeUnsupportedField || PathOf(err) != "background" {
			t.Fatalf("Decode() = code:%q path:%q", CodeOf(err), PathOf(err))
		}
	}
}

func TestDecodeRejectsOversizedBodyBeforeParsing(t *testing.T) {
	body := strings.Repeat("x", MaxRequestBodyBytes+1)
	_, err := Decode(strings.NewReader(body), &sequenceIDs{})
	if CodeOf(err) != CodeLimitExceeded || PathOf(err) != "request" {
		t.Fatalf("Decode() = %v", err)
	}
}
