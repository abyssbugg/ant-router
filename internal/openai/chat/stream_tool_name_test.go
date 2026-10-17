package chat

import (
	"errors"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// runToolStream feeds raw chunks through an accumulator and a streaming
// machine, returning every completed item or the first accumulator error.
func runToolStream(t *testing.T, chunks []string) ([]canonical.Item, error) {
	t.Helper()
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	accumulator := NewAccumulator(responseID, "public/model", ids)
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	var items []canonical.Item
	feed := func(events []canonical.Event) {
		for _, event := range events {
			if _, err := machine.Accept(event); err != nil {
				t.Fatalf("machine rejected %q: %v", event.Type(), err)
			}
			if completed, ok := event.ItemCompleted(); ok {
				items = append(items, completed.Item())
			}
		}
	}
	for _, raw := range chunks {
		chunk, err := DecodeChunk([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		events, err := accumulator.Accept(chunk)
		if err != nil {
			return nil, err
		}
		feed(events)
	}
	events, err := accumulator.Finish()
	if err != nil {
		return nil, err
	}
	feed(events)
	if machine.State() != streaming.StateTerminal {
		t.Fatalf("machine state = %q", machine.State())
	}
	return items, nil
}

const toolChunkRole = `{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant"}}]}`
const toolChunkFinish = `{"id":"c1","choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`

func protocolPath(err error) string {
	var target *Error
	if errors.As(err, &target) && target.Code == "protocol" {
		return target.Path
	}
	return ""
}

func toolChunk(fields string) string {
	return `{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,` + fields + `}]}}]}`
}

func TestAccumulatorJoinsToolNameSplitAcrossChunks(t *testing.T) {
	items, err := runToolStream(t, []string{
		toolChunkRole,
		toolChunk(`"id":"up_1","function":{"name":"get_","arguments":""}`),
		toolChunk(`"function":{"name":"current_"}`),
		toolChunk(`"function":{"name":"weather"}`),
		toolChunk(`"function":{"arguments":"{\"city\":"}`),
		toolChunk(`"function":{"arguments":"\"Oslo\"}"}`),
		toolChunkFinish,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	call, ok := items[0].ToolCall()
	if !ok || call.Name() != "get_current_weather" {
		t.Fatalf("tool call = %#v ok=%t, want name get_current_weather", call, ok)
	}
	if got := string(call.Arguments().Bytes()); got != `{"city":"Oslo"}` {
		t.Fatalf("arguments = %s", got)
	}
}

func TestAccumulatorStartsNameOnlyToolAtCompletion(t *testing.T) {
	items, err := runToolStream(t, []string{
		toolChunkRole,
		toolChunk(`"id":"up_1","function":{"name":"list_"}`),
		toolChunk(`"function":{"name":"files"}`),
		toolChunk(`"function":{"name":"","arguments":""}`),
		toolChunkFinish,
	})
	if err != nil {
		t.Fatalf("runToolStream failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	call, ok := items[0].ToolCall()
	if !ok {
		t.Fatalf("item is not a tool call")
	}
	if call.Name() != "list_files" {
		t.Fatalf("tool call name = %q, want list_files", call.Name())
	}
	if got := string(call.Arguments().Bytes()); got != "{}" {
		t.Fatalf("arguments = %q, want {}", got)
	}
}

func TestAccumulatorRejectsNameFragmentAfterArguments(t *testing.T) {
	_, err := runToolStream(t, []string{
		toolChunkRole,
		toolChunk(`"id":"up_1","function":{"name":"lookup","arguments":"{"}`),
		toolChunk(`"function":{"name":"_more","arguments":"}"}`),
		toolChunkFinish,
	})
	if got := protocolPath(err); got != "tool.name" {
		t.Fatalf("error = %v (path %q), want tool.name protocol error", err, got)
	}
}

func TestAccumulatorRejectsOverlongSplitToolName(t *testing.T) {
	half := make([]byte, canonical.MaxToolNameBytes/2+1)
	for i := range half {
		half[i] = 'a'
	}
	_, err := runToolStream(t, []string{
		toolChunkRole,
		toolChunk(`"id":"up_1","function":{"name":"` + string(half) + `"}`),
		toolChunk(`"function":{"name":"` + string(half) + `"}`),
		toolChunkFinish,
	})
	if got := protocolPath(err); got != "tool.name" {
		t.Fatalf("error = %v (path %q), want tool.name protocol error", err, got)
	}
}
