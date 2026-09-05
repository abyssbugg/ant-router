package anthropic

import (
	"fmt"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestMapRequestOmitsToolChoiceWithoutTools(t *testing.T) {
	params, err := mapRequest(decodeRequest(t, `{"model":"anthropic/public","input":"hello"}`), "claude-upstream", 1024, 4096)
	if err != nil {
		t.Fatalf("mapRequest: %v", err)
	}
	object := requestObject(t, params)
	if _, exists := object["tool_choice"]; exists {
		t.Fatalf("tool_choice emitted without tools: %#v", object["tool_choice"])
	}
}

func TestMapRequestRejectsInvalidConfiguredTokenBounds(t *testing.T) {
	request := decodeRequest(t, `{"model":"anthropic/public","input":"hello"}`)
	tests := []struct{ defaultTokens, maxTokens int64 }{
		{0, 4096}, {4097, 4096}, {1024, int64(canonical.MaxOutputTokens) + 1},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d_%d", test.defaultTokens, test.maxTokens), func(t *testing.T) {
			if _, err := mapRequest(request, "claude-upstream", test.defaultTokens, test.maxTokens); err == nil {
				t.Fatal("expected invalid configured token bounds")
			}
		})
	}
}

func TestMapRequestRejectsLateInstructionsAndNonAutoImageDetail(t *testing.T) {
	tests := map[string]string{
		"late system": `{"model":"anthropic/public","input":[{"role":"user","content":"hello"},{"role":"system","content":"late"}]}`,
		"high detail": `{"model":"anthropic/public","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/x.png","detail":"high"}]}]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := mapRequest(decodeRequest(t, body), "claude-upstream", 1024, 4096); err == nil {
				t.Fatal("expected request rejection")
			}
		})
	}
}

func TestMapRequestCoalescesAdjacentRoles(t *testing.T) {
	request := decodeRequest(t, `{"model":"anthropic/public","input":[{"role":"user","content":"one"},{"role":"user","content":"two"},{"role":"assistant","content":"three"}]}`)
	params, err := mapRequest(request, "claude-upstream", 1024, 4096)
	if err != nil {
		t.Fatalf("mapRequest: %v", err)
	}
	messages := asArray(t, requestObject(t, params)["messages"], "messages")
	if len(messages) != 2 || len(asArray(t, asObject(t, messages[0], "messages[0]")["content"], "content")) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
}
