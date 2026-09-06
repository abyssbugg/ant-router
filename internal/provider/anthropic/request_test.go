package anthropic

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	inbound "github.com/abyssbugg/ant-router/internal/openai/responses"
)

type requestIDs struct{ next byte }

func (g *requestIDs) reader() *bytes.Reader {
	g.next++
	return bytes.NewReader(bytes.Repeat([]byte{g.next}, 16))
}
func (g *requestIDs) RequestID() (canonical.RequestID, error) {
	return canonical.GenerateRequestID(g.reader())
}
func (g *requestIDs) ItemID() (canonical.ItemID, error) {
	return canonical.GenerateItemID(g.reader())
}
func (g *requestIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(g.reader())
}

func decodeRequest(t *testing.T, body string) canonical.Request {
	t.Helper()
	parsed, err := inbound.Decode(bytes.NewBufferString(body), &requestIDs{})
	if err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return parsed.Request
}

func requestObject(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	return object
}

func asObject(t *testing.T, value any, path string) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want object", path, value)
	}
	return object
}

func asArray(t *testing.T, value any, path string) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %T, want array", path, value)
	}
	return array
}
