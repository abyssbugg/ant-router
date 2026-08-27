package mock

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

func TestCatalogIsExactSortedAndCapabilityAware(t *testing.T) {
	provider, err := New(Options{IDs: &sequenceIDs{}, SlowDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	models, err := provider.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mock/disconnect", "mock/json", "mock/reasoning", "mock/slow", "mock/text", "mock/tool"}
	if len(models) != len(want) {
		t.Fatalf("model count = %d", len(models))
	}
	for index := range want {
		if models[index].ID() != want[index] || models[index].Provider() != "mock" {
			t.Fatalf("model[%d] = %q/%q", index, models[index].Provider(), models[index].ID())
		}
	}
	if !models[1].Capabilities().Has(canonical.CapabilityStructuredOutput) || !models[2].Capabilities().Has(canonical.CapabilityReasoningSummary) || !models[5].Capabilities().Has(canonical.CapabilityTools) {
		t.Fatal("scenario capabilities are incomplete")
	}
}

func TestSuccessfulScenariosProduceValidTerminalStreams(t *testing.T) {
	provider, _ := New(Options{IDs: &sequenceIDs{}, SlowDelay: time.Millisecond})
	for _, model := range []string{"mock/text", "mock/json", "mock/reasoning", "mock/tool", "mock/slow"} {
		t.Run(model, func(t *testing.T) {
			request := scenarioRequest(t, model)
			source, err := provider.Execute(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			response := reduceSource(t, source)
			if response.Model() != model || len(response.Items()) != 1 {
				t.Fatalf("response = %#v", response)
			}
			if model == "mock/tool" && response.FinishReason() != canonical.FinishReason("tool_call") {
				t.Fatalf("tool finish = %q", response.FinishReason())
			}
			if model != "mock/tool" && response.FinishReason() != canonical.FinishReason("stop") {
				t.Fatalf("finish = %q", response.FinishReason())
			}
			if err := source.Close(); err != nil {
				t.Fatal(err)
			}
			if err := source.Close(); err != nil {
				t.Fatalf("second close: %v", err)
			}
		})
	}
}

func TestDisconnectFailsBeforeSemanticOutput(t *testing.T) {
	provider, _ := New(Options{IDs: &sequenceIDs{}})
	source, err := provider.Execute(context.Background(), scenarioRequest(t, "mock/disconnect"))
	if err != nil {
		t.Fatal(err)
	}
	machine, _ := streaming.NewMachine(streaming.DefaultMachineOptions())
	first, err := source.Recv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.Accept(first); err != nil {
		t.Fatal(err)
	}
	_, err = source.Recv(context.Background())
	if execution.CodeOf(err) != execution.CodeUnavailable || machine.Snapshot().Committed() {
		t.Fatalf("disconnect = %v, snapshot = %#v", err, machine.Snapshot())
	}
}

func TestSlowScenarioHonorsCancellation(t *testing.T) {
	provider, _ := New(Options{IDs: &sequenceIDs{}, SlowDelay: time.Second})
	source, err := provider.Execute(context.Background(), scenarioRequest(t, "mock/slow"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Recv(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Recv error = %v", err)
	}
}

func TestExecuteRejectsUnknownAndUnsupportedModels(t *testing.T) {
	provider, _ := New(Options{IDs: &sequenceIDs{}})
	unknown := scenarioRequest(t, "mock/unknown")
	if _, err := provider.Execute(context.Background(), unknown); execution.CodeOf(err) != execution.CodeInvalidModel {
		t.Fatalf("unknown model error = %v", err)
	}
	toolRequest := scenarioRequest(t, "mock/tool")
	textRequest := cloneRequestModel(t, toolRequest, "mock/text")
	if _, err := provider.Execute(context.Background(), textRequest); execution.CodeOf(err) != execution.CodeInvalidModel {
		t.Fatalf("unsupported capability error = %v", err)
	}
}

func reduceSource(t *testing.T, source streaming.EventSource) canonical.Response {
	t.Helper()
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	for {
		event, recvErr := source.Recv(context.Background())
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				t.Fatal("source ended without terminal event")
			}
			t.Fatal(recvErr)
		}
		transition, acceptErr := machine.Accept(event)
		if acceptErr != nil {
			t.Fatal(acceptErr)
		}
		if response, ok := transition.Response(); ok {
			return response
		}
	}
}

func scenarioRequest(t *testing.T, model string) canonical.Request {
	t.Helper()
	ids := &sequenceIDs{}
	text, _ := canonical.NewTextContent("hello")
	block, _ := canonical.NewTextBlock(text)
	itemID, _ := ids.ItemID()
	message, _ := canonical.NewMessage(itemID, canonical.Role("user"), []canonical.ContentBlock{block})
	item, _ := canonical.NewMessageItem(message)
	choice, _ := canonical.NewToolChoice(canonical.ToolChoiceKind("auto"), "")
	output, _ := canonical.NewOutputConstraints(canonical.OutputParams{Format: canonical.OutputFormat("text")})
	params := canonical.RequestParams{Model: model, Input: []canonical.Item{item}, ToolChoice: choice, Output: output, Sampling: func() canonical.Sampling { value, _ := canonical.NewSampling(canonical.SamplingParams{}); return value }()}
	params.ID, _ = ids.RequestID()
	if model == "mock/tool" {
		schema, _ := canonicaljson.Parse([]byte(`{"type":"object"}`), canonicaljson.DefaultLimits())
		tool, _ := canonical.NewToolDefinition("Search", "", schema)
		params.Tools = []canonical.ToolDefinition{tool}
	}
	if model == "mock/json" {
		params.Output, _ = canonical.NewOutputConstraints(canonical.OutputParams{Format: canonical.OutputFormat("json_object")})
	}
	if model == "mock/reasoning" {
		params.ReasoningSummary = canonical.ReasoningSummaryAuto
	}
	request, err := canonical.NewRequest(params)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func cloneRequestModel(t *testing.T, base canonical.Request, model string) canonical.Request {
	t.Helper()
	request, err := canonical.NewRequest(canonical.RequestParams{ID: base.ID(), Model: model, Input: base.Input(), Tools: base.Tools(), ToolChoice: base.ToolChoice(), Output: base.Output(), Sampling: base.Sampling(), ReasoningSummary: base.ReasoningSummary()})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

type sequenceIDs struct{ next byte }

func (s *sequenceIDs) reader() *bytes.Reader {
	s.next++
	return bytes.NewReader(bytes.Repeat([]byte{s.next}, 16))
}
func (s *sequenceIDs) RequestID() (canonical.RequestID, error) {
	return canonical.GenerateRequestID(s.reader())
}
func (s *sequenceIDs) ResponseID() (canonical.ResponseID, error) {
	return canonical.GenerateResponseID(s.reader())
}
func (s *sequenceIDs) EventID() (canonical.EventID, error) {
	return canonical.GenerateEventID(s.reader())
}
func (s *sequenceIDs) ItemID() (canonical.ItemID, error) { return canonical.GenerateItemID(s.reader()) }
func (s *sequenceIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(s.reader())
}
