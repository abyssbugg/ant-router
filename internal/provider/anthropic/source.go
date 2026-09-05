package anthropic

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"reflect"
	"sync"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

type sdkEventStream interface {
	Next() bool
	Current() sdk.MessageStreamEventUnion
	Err() error
	Close() error
}

type streamIDGenerator interface {
	ResponseID() (canonical.ResponseID, error)
	EventID() (canonical.EventID, error)
	ItemID() (canonical.ItemID, error)
	ToolCallID() (canonical.ToolCallID, error)
}

type blockState struct {
	index        int
	kind         string
	itemID       canonical.ItemID
	contentIndex int
	callID       canonical.ToolCallID
	name         string
	text         string
	arguments    string
	completed    bool
}

type eventSource struct {
	stream      sdkEventStream
	publicModel string
	format      canonical.OutputFormat
	ids         streamIDGenerator

	responseID         canonical.ResponseID
	upstreamID         string
	sequence           uint64
	started            bool
	terminal           bool
	failed             bool
	pending            []canonical.Event
	blocks             map[int]*blockState
	nextBlock          int
	messageID          canonical.ItemID
	messageIndex       int
	messageStarted     bool
	messageContents    map[int]canonical.ContentBlock
	nextMessageContent int
	orderedItemIDs     []canonical.ItemID
	latestUsage        canonical.Usage
	finish             canonical.FinishReason
	hasFinish          bool

	closeOnce sync.Once
	closeErr  error
}

func newEventSource(stream sdkEventStream, publicModel string, format canonical.OutputFormat, ids streamIDGenerator) (*eventSource, error) {
	if nilInterface(stream) || publicModel == "" || !format.Valid() || nilInterface(ids) {
		return nil, streamError("stream.options")
	}
	return &eventSource{stream: stream, publicModel: publicModel, format: format, ids: ids, blocks: make(map[int]*blockState), messageContents: make(map[int]canonical.ContentBlock)}, nil
}

func (s *eventSource) Recv(ctx context.Context) (canonical.Event, error) {
	if s == nil || ctx == nil {
		return canonical.Event{}, streamError("stream.context")
	}
	if err := ctx.Err(); err != nil {
		return canonical.Event{}, err
	}
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		if event.Type() == canonical.EventType("response_completed") {
			s.terminal = true
		}
		return event, nil
	}
	if s.failed || s.terminal {
		return canonical.Event{}, io.EOF
	}
	for {
		if err := ctx.Err(); err != nil {
			return canonical.Event{}, err
		}
		if !s.stream.Next() {
			if err := s.stream.Err(); err != nil {
				return canonical.Event{}, s.fail(normalizeProviderError(err))
			}
			return canonical.Event{}, s.fail(streamError("stream.eof"))
		}
		event, emit, err := s.mapEvent(s.stream.Current())
		if err != nil {
			return canonical.Event{}, s.fail(err)
		}
		if emit {
			return event, nil
		}
	}
}

func (s *eventSource) mapEvent(upstream sdk.MessageStreamEventUnion) (canonical.Event, bool, error) {
	switch upstream.Type {
	case "ping":
		return canonical.Event{}, false, nil
	case "message_start":
		return s.mapMessageStart(upstream.AsMessageStart())
	case "content_block_start":
		return s.mapBlockStart(upstream.AsContentBlockStart())
	case "content_block_delta":
		return s.mapBlockDelta(upstream.AsContentBlockDelta())
	case "content_block_stop":
		return s.mapBlockStop(upstream.AsContentBlockStop())
	case "message_delta":
		return s.mapMessageDelta(upstream.AsMessageDelta())
	case "message_stop":
		return s.mapMessageStop()
	default:
		return canonical.Event{}, false, streamError("stream.event")
	}
}

func (s *eventSource) mapMessageStart(event sdk.MessageStartEvent) (canonical.Event, bool, error) {
	if s.started || event.Message.ID == "" || event.Message.Model == "" || event.Message.Role != "assistant" || len(event.Message.Content) != 0 || event.Message.StopReason != "" || event.Message.StopSequence != "" {
		return canonical.Event{}, false, streamError("stream.message_start")
	}
	responseID, err := s.ids.ResponseID()
	if err != nil {
		return canonical.Event{}, false, streamError("stream.id")
	}
	started, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewResponseStartedEvent(id, responseID, sequence, s.publicModel)
	})
	if err != nil {
		return canonical.Event{}, false, streamError("stream.canonical")
	}
	usage, err := mapUsage(event.Message.Usage, sdk.MessageDeltaUsage{})
	if err != nil {
		return canonical.Event{}, false, err
	}
	s.responseID, s.upstreamID, s.started, s.latestUsage = responseID, event.Message.ID, true, usage
	if usage.TotalTokens() > 0 {
		usageEvent, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
			return canonical.NewUsageUpdatedEvent(id, responseID, sequence, usage)
		})
		if err != nil {
			return canonical.Event{}, false, streamError("stream.canonical")
		}
		s.pending = append(s.pending, usageEvent)
	}
	return started, true, nil
}

func (s *eventSource) newEvent(build func(canonical.EventID, uint64) (canonical.Event, error)) (canonical.Event, error) {
	id, err := s.ids.EventID()
	if err != nil {
		return canonical.Event{}, err
	}
	event, err := build(id, s.sequence)
	if err == nil {
		s.sequence++
	}
	return event, err
}

func (s *eventSource) fail(err error) error { s.failed = true; _ = s.Close(); return err }
func (s *eventSource) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if err := s.stream.Close(); err != nil {
			s.closeErr = normalizeProviderError(err)
		}
	})
	return s.closeErr
}

func streamError(path string) error {
	return execution.NewError(execution.CodeInternal, path, 0, errors.New("provider stream is invalid"))
}
func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

type randomStreamIDs struct{}

func (randomStreamIDs) ResponseID() (canonical.ResponseID, error) {
	return canonical.GenerateResponseID(rand.Reader)
}
func (randomStreamIDs) EventID() (canonical.EventID, error) {
	return canonical.GenerateEventID(rand.Reader)
}
func (randomStreamIDs) ItemID() (canonical.ItemID, error) {
	return canonical.GenerateItemID(rand.Reader)
}
func (randomStreamIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(rand.Reader)
}

var _ streaming.EventSource = (*eventSource)(nil)
