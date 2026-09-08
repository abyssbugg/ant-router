package responses

import (
	"strings"
	"testing"
)

func TestDecodeCurrentCodexHostedWebSearchDeclaration(t *testing.T) {
	request, err := Decode(strings.NewReader(`{"model":"mock/codex","input":"hi","tools":[{"type":"web_search","external_web_access":true,"indexed_web_access":false,"filters":{"allowed_domains":["example.com"]},"user_location":{"type":"approximate","country":"US"},"search_context_size":"medium","search_content_types":["text","news"]}]}`), &sequenceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	tools := request.Request.Tools()
	if len(tools) != 1 || tools[0].Kind() != "web_search" {
		t.Fatalf("tools = %#v", tools)
	}
	options := tools[0].WebSearchOptions()
	if value, set := options.ExternalWebAccess(); !set || !value {
		t.Fatal("external access lost")
	}
	if value, set := options.IndexedWebAccess(); !set || value {
		t.Fatal("indexed access lost")
	}
	if options.SearchContextSize() != "medium" || len(options.SearchContentTypes()) != 2 {
		t.Fatalf("search options = %#v", options)
	}
	if string(options.Filters().Bytes()) != `{"allowed_domains":["example.com"]}` || string(options.UserLocation().Bytes()) != `{"country":"US","type":"approximate"}` {
		t.Fatal("search objects lost")
	}
}

func TestDecodeRejectsHostedWebSearchFields(t *testing.T) {
	for _, body := range []string{
		`{"model":"mock/codex","input":"hi","tools":[{"type":"web_search","name":"search"}]}`,
		`{"model":"mock/codex","input":"hi","tools":[{"type":"web_search","tools":[]}]}`,
	} {
		if _, err := Decode(strings.NewReader(body), &sequenceIDs{}); err == nil {
			t.Fatal("malformed hosted search accepted")
		}
	}
}
