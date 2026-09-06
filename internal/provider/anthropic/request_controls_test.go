package anthropic

import (
	"encoding/json"
	"testing"
)

func TestMapRequestPreservesControlsAndConfiguredLimits(t *testing.T) {
	request := decodeRequest(t, `{
		"model":"anthropic/public",
		"input":[
			{"role":"system","content":"system"},
			{"role":"developer","content":"developer"},
			{"role":"user","content":"find docs"}
		],
		"tools":[{"type":"function","name":"Search","description":"find docs","parameters":{"type":"object","properties":{"limit":{"type":"integer","maximum":9007199254740993}}},"strict":true}],
		"tool_choice":{"type":"function","name":"Search"},
		"parallel_tool_calls":true,
		"text":{"format":{"type":"json_schema","name":"Answer","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}},"strict":true}},
		"temperature":0.2,
		"top_p":0.9,
		"max_output_tokens":512
	}`)
	params, err := mapRequest(request, "claude-upstream", 1024, 4096)
	if err != nil {
		t.Fatalf("mapRequest: %v", err)
	}
	object := requestObject(t, params)
	if object["model"] != "claude-upstream" || object["max_tokens"] != json.Number("512") || object["temperature"] != json.Number("0.2") || object["top_p"] != json.Number("0.9") {
		t.Fatalf("controls = %#v", object)
	}
	system := asArray(t, object["system"], "system")
	if len(system) != 2 || asObject(t, system[0], "system[0]")["text"] != "system" || asObject(t, system[1], "system[1]")["text"] != "developer" {
		t.Fatalf("system = %#v", system)
	}
	tools := asArray(t, object["tools"], "tools")
	tool := asObject(t, tools[0], "tools[0]")
	if tool["name"] != "Search" || tool["description"] != "find docs" || tool["strict"] != true {
		t.Fatalf("tool = %#v", tool)
	}
	maximum := asObject(t, asObject(t, asObject(t, tool["input_schema"], "input_schema")["properties"], "properties")["limit"], "limit")["maximum"]
	if maximum != json.Number("9007199254740993") {
		t.Fatalf("schema maximum = %v (%T)", maximum, maximum)
	}
	choice := asObject(t, object["tool_choice"], "tool_choice")
	if choice["type"] != "tool" || choice["name"] != "Search" || choice["disable_parallel_tool_use"] != false {
		t.Fatalf("tool_choice = %#v", choice)
	}
	format := asObject(t, asObject(t, object["output_config"], "output_config")["format"], "format")
	if format["type"] != "json_schema" || asObject(t, format["schema"], "schema")["type"] != "object" {
		t.Fatalf("output format = %#v", format)
	}
}

func TestMapRequestUsesConfiguredDefaultAndRejectsExcess(t *testing.T) {
	request := decodeRequest(t, `{"model":"anthropic/public","input":"hello"}`)
	params, err := mapRequest(request, "claude-upstream", 1024, 4096)
	if err != nil || params.MaxTokens != 1024 {
		t.Fatalf("default max tokens = %d, err=%v", params.MaxTokens, err)
	}
	excess := decodeRequest(t, `{"model":"anthropic/public","input":"hello","max_output_tokens":4097}`)
	if _, err := mapRequest(excess, "claude-upstream", 1024, 4096); err == nil {
		t.Fatal("expected configured maximum rejection")
	}
}
