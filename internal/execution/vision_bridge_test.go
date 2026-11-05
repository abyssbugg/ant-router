package execution

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// deterministicIDs generates unique canonical identifiers from a counter.
type deterministicIDs struct{ counter int }

func (d *deterministicIDs) next() io.Reader {
	d.counter++
	return strings.NewReader(strings.Repeat(string(rune(d.counter)), 64))
}
func (d *deterministicIDs) ItemID() (canonical.ItemID, error) {
	return canonical.GenerateItemID(d.next())
}
func (d *deterministicIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(d.next())
}
func (d *deterministicIDs) EventID() (canonical.EventID, error) {
	return canonical.GenerateEventID(d.next())
}
func (d *deterministicIDs) ResponseID() (canonical.ResponseID, error) {
	return canonical.GenerateResponseID(d.next())
}
func (d *deterministicIDs) RequestID() (canonical.RequestID, error) {
	return canonical.GenerateRequestID(d.next())
}

// textEventStream emits one machine-valid canonical event stream that
// reconstructs a single assistant message with the given text.
type textEventStream struct {
	events []canonical.Event
	cursor int
}

func newTextEventStream(model, text string) (*textEventStream, error) {
	ids := &deterministicIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		return nil, err
	}
	newID := func() canonical.EventID {
		id, err := ids.EventID()
		if err != nil {
			return ""
		}
		return id
	}
	itemID, err := ids.ItemID()
	if err != nil {
		return nil, err
	}
	start, err := canonical.NewResponseStartedEvent(newID(), responseID, 0, model)
	if err != nil {
		return nil, err
	}
	itemStart, err := canonical.NewItemStartedEvent(newID(), responseID, 1, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	if err != nil {
		return nil, err
	}
	contentStart, err := canonical.NewContentStartedEvent(newID(), responseID, 2, itemID, 0, canonical.ContentType("text"))
	if err != nil {
		return nil, err
	}
	textDelta, err := canonical.NewTextDeltaEvent(newID(), responseID, 3, itemID, 0, text)
	if err != nil {
		return nil, err
	}
	content, err := canonical.NewTextContent(text)
	if err != nil {
		return nil, err
	}
	block, err := canonical.NewTextBlock(content)
	if err != nil {
		return nil, err
	}
	contentDone, err := canonical.NewContentCompletedEvent(newID(), responseID, 4, itemID, 0, block)
	if err != nil {
		return nil, err
	}
	message, err := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
	if err != nil {
		return nil, err
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		return nil, err
	}
	itemDone, err := canonical.NewItemCompletedEvent(newID(), responseID, 5, 0, item)
	if err != nil {
		return nil, err
	}
	completed, err := canonical.NewResponseCompletedEvent(newID(), responseID, 6, canonical.FinishReason("stop"), canonical.Usage{}, []canonical.ItemID{itemID})
	if err != nil {
		return nil, err
	}
	return &textEventStream{events: []canonical.Event{start, itemStart, contentStart, textDelta, contentDone, itemDone, completed}}, nil
}

func (s *textEventStream) Recv(context.Context) (canonical.Event, error) {
	if s.cursor >= len(s.events) {
		return canonical.Event{}, io.EOF
	}
	event := s.events[s.cursor]
	s.cursor++
	return event, nil
}

func (s *textEventStream) Close() error { return nil }

var _ streaming.EventSource = (*textEventStream)(nil)

// fakeBridgeExecutor records every request and streams scripted text back.
type fakeBridgeExecutor struct {
	models   []Model
	requests []canonical.Request
	streams  []*textEventStream
	failAt   int // 1-based call index that fails; 0 = never
}

func (f *fakeBridgeExecutor) Execute(ctx context.Context, request canonical.Request) (streaming.EventSource, error) {
	f.requests = append(f.requests, request)
	if f.failAt != 0 && len(f.requests) == f.failAt {
		return nil, errors.New("vision model unavailable")
	}
	stream, err := newTextEventStream(request.Model(), "a red cube on a blue table")
	if err != nil {
		return nil, err
	}
	f.streams = append(f.streams, stream)
	return stream, nil
}

func (f *fakeBridgeExecutor) Models(context.Context) ([]Model, error) {
	return append([]Model(nil), f.models...), nil
}

func visionModel(t *testing.T, id string) Model {
	t.Helper()
	set, err := canonical.NewCapabilitySet(canonical.CapabilityText, canonical.CapabilityImageInput)
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewModel(ModelParams{ID: id, Provider: "vision", Capabilities: set})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func textModel(t *testing.T, id string) Model {
	t.Helper()
	set, err := canonical.NewCapabilitySet(canonical.CapabilityText)
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewModel(ModelParams{ID: id, Provider: "text", Capabilities: set})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func imageMessageItem(t *testing.T, images int) canonical.Item {
	t.Helper()
	content, err := canonical.NewTextContent("what is in the picture?")
	if err != nil {
		t.Fatal(err)
	}
	blocks := []canonical.ContentBlock{mustBlock(t, content)}
	for index := 0; index < images; index++ {
		image, err := canonical.NewRemoteImage("https://example.com/pic.png", "image/png", canonical.ImageDetail("low"), 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		blocks = append(blocks, mustImageBlock(t, image))
	}
	message, err := canonical.NewMessage(itemIDFor(t, 10), canonical.Role("user"), blocks)
	if err != nil {
		t.Fatal(err)
	}
	return mustMessageItem(t, message)
}

func itemIDFor(t *testing.T, value byte) canonical.ItemID {
	t.Helper()
	id, err := canonical.GenerateItemID(strings.NewReader(strings.Repeat(string(rune(value)), 64)))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func requestIDFor(t *testing.T, value byte) canonical.RequestID {
	t.Helper()
	id, err := canonical.GenerateRequestID(strings.NewReader(strings.Repeat(string(rune(value)), 64)))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustBlock(t *testing.T, content canonical.TextContent) canonical.ContentBlock {
	t.Helper()
	block, err := canonical.NewTextBlock(content)
	if err != nil {
		t.Fatal(err)
	}
	return block
}

func mustImageBlock(t *testing.T, image canonical.ImageContent) canonical.ContentBlock {
	t.Helper()
	block, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatal(err)
	}
	return block
}

func mustMessageItem(t *testing.T, message canonical.Message) canonical.Item {
	t.Helper()
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func imagelessVariant(t *testing.T, request canonical.Request) canonical.Request {
	t.Helper()
	content, err := canonical.NewTextContent("what is in the picture?")
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessage(itemIDFor(t, 10), canonical.Role("user"), []canonical.ContentBlock{mustBlock(t, content)})
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := canonical.NewRequest(canonical.RequestParams{ID: request.ID(), Model: request.Model(), Input: []canonical.Item{mustMessageItem(t, message)}})
	if err != nil {
		t.Fatal(err)
	}
	return rebuilt
}

func TestVisionBridgeReplacesImagesWithDescriptions(t *testing.T) {
	inner := &fakeBridgeExecutor{models: []Model{visionModel(t, "vision-model"), textModel(t, "text-model")}}
	bridge := NewVisionBridge(inner, "vision-model")
	request, err := canonical.NewRequest(canonical.RequestParams{
		ID: requestIDFor(t, 1), Model: "text-model",
		Input: []canonical.Item{imageMessageItem(t, 2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	source, err := bridge.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	_ = source.Close()
	if len(inner.requests) != 2 {
		t.Fatalf("inner calls = %d, want 2 (describe then final)", len(inner.requests))
	}
	describe := inner.requests[0]
	if describe.Model() != "vision-model" {
		t.Fatalf("describe model = %q, want vision-model", describe.Model())
	}
	if describe.Capabilities().Has(canonical.CapabilityImageInput) == false {
		t.Fatal("describe request lost its images")
	}
	if len(describe.Tools()) != 0 {
		t.Fatal("describe request carried tools")
	}
	final := inner.requests[1]
	if final.Model() != "text-model" {
		t.Fatalf("final model = %q, want text-model", final.Model())
	}
	if final.Capabilities().Has(canonical.CapabilityImageInput) {
		t.Fatal("final request still requires image_input")
	}
	joined := finalInputText(t, final)
	if !strings.Contains(joined, "a red cube on a blue table") {
		t.Fatalf("final input missing the description text: %q", joined)
	}
	describeText := finalInputText(t, describe)
	if !strings.Contains(describeText, "Begin each description with 'Image 1:'") && !strings.Contains(describeText, "Begin each description with 'Image N:'") {
		t.Fatalf("describe request missing the numbering instruction: %q", describeText)
	}
	if strings.Contains(joined, "https://example.com/pic.png") {
		t.Fatal("final input still contains the image URL")
	}
}

func finalInputText(t *testing.T, request canonical.Request) string {
	t.Helper()
	text := ""
	for _, item := range request.Input() {
		message, ok := item.Message()
		if !ok {
			continue
		}
		for _, block := range message.Content() {
			if value, ok := block.Text(); ok {
				text += value.Text() + "\n"
			}
		}
	}
	return text
}

func TestVisionBridgePassesThroughNonImageRequests(t *testing.T) {
	inner := &fakeBridgeExecutor{models: []Model{textModel(t, "text-model")}}
	bridge := NewVisionBridge(inner, "vision-model")
	request := imagelessVariant(t, mustImageRequest(t))
	if _, err := bridge.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(inner.requests) != 1 {
		t.Fatalf("inner calls = %d, want 1 (no describe)", len(inner.requests))
	}
}

func mustImageRequest(t *testing.T) canonical.Request {
	t.Helper()
	request, err := canonical.NewRequest(canonical.RequestParams{
		ID: requestIDFor(t, 2), Model: "text-model",
		Input: []canonical.Item{imageMessageItem(t, 1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestVisionBridgePassesThroughVisionCapableTarget(t *testing.T) {
	inner := &fakeBridgeExecutor{models: []Model{visionModel(t, "vision-model"), visionModel(t, "text-model")}}
	bridge := NewVisionBridge(inner, "vision-model")
	request := mustImageRequest(t)
	if _, err := bridge.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(inner.requests) != 1 {
		t.Fatalf("inner calls = %d, want 1 (target is vision capable)", len(inner.requests))
	}
}

func TestVisionBridgeFailsWhenBridgeModelUnavailable(t *testing.T) {
	inner := &fakeBridgeExecutor{models: []Model{textModel(t, "text-model")}}
	bridge := NewVisionBridge(inner, "missing-model")
	_, err := bridge.Execute(context.Background(), mustImageRequest(t))
	if err == nil {
		t.Fatal("missing bridge model accepted")
	}
	if CodeOf(err) != CodeInvalidModel {
		t.Fatalf("error = %v, want invalid_model", err)
	}
}

func TestVisionBridgeFailsWhenDescribeCallFails(t *testing.T) {
	inner := &fakeBridgeExecutor{models: []Model{visionModel(t, "vision-model"), textModel(t, "text-model")}, failAt: 1}
	bridge := NewVisionBridge(inner, "vision-model")
	_, err := bridge.Execute(context.Background(), mustImageRequest(t))
	if err == nil {
		t.Fatal("describe failure accepted")
	}
	if len(inner.requests) != 1 {
		t.Fatalf("inner calls = %d, want 1 (no final call after describe failure)", len(inner.requests))
	}
}

func TestVisionBridgeInjectsDescriptionOnce(t *testing.T) {
	inner := &fakeBridgeExecutor{models: []Model{visionModel(t, "vision-model"), textModel(t, "text-model")}}
	bridge := NewVisionBridge(inner, "vision-model")
	request := mustImageRequest(t)
	if _, err := bridge.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	final := inner.requests[1]
	count := 0
	for _, item := range final.Input() {
		message, ok := item.Message()
		if !ok {
			continue
		}
		for _, block := range message.Content() {
			if value, ok := block.Text(); ok && strings.Contains(value.Text(), "a red cube on a blue table") {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("description injected %d times, want exactly once", count)
	}
}

func TestVisionBridgePreservesContinuation(t *testing.T) {
	inner := &fakeBridgeExecutor{models: []Model{visionModel(t, "vision-model"), textModel(t, "text-model")}}
	bridge := NewVisionBridge(inner, "vision-model")
	continuation, err := canonical.NewContinuation("text", "upstream-text", []byte(`{"entries":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	request, err := canonical.NewRequest(canonical.RequestParams{
		ID: requestIDFor(t, 3), Model: "text-model",
		Input:        []canonical.Item{imageMessageItem(t, 1)},
		Continuation: &continuation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(inner.requests) != 2 {
		t.Fatalf("inner calls = %d, want 2", len(inner.requests))
	}
	final := inner.requests[1]
	preserved := final.Continuation()
	if preserved == nil || preserved.Provider() != continuation.Provider() || preserved.Model() != continuation.Model() || string(preserved.Bytes()) != string(continuation.Bytes()) {
		t.Fatalf("final continuation = %#v, want preserved", preserved)
	}
}
