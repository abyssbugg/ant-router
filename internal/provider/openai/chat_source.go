package openai

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/openai/chat"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// chatStarter performs one retry-free Chat Completions stream attempt.
type chatStarter interface {
	Start(ctx context.Context, secret credential.Secret, body []byte) (*http.Response, error)
}

type httpChatStarter struct {
	factory *clientFactory
}

func (s *httpChatStarter) Start(ctx context.Context, secret credential.Secret, body []byte) (*http.Response, error) {
	if s == nil || s.factory == nil {
		return nil, errors.New("invalid chat stream starter")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.factory.BaseURL()+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+secret.Reveal())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	return s.factory.HTTPClient().Do(request)
}

// codeForChatStatus maps a chat endpoint HTTP status to a stable execution code.
func codeForChatStatus(status int) execution.ErrorCode {
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		return execution.CodeInvalidRequest
	case http.StatusUnauthorized:
		return execution.CodeAuthenticationFailed
	case http.StatusForbidden:
		return execution.CodePermissionDenied
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly:
		return execution.CodeUnavailable
	case http.StatusTooManyRequests:
		return execution.CodeRateLimited
	default:
		if status >= 500 && status <= 599 {
			return execution.CodeUnavailable
		}
		return execution.CodeInternal
	}
}

const maxChatSSELine = 1 << 20

// chatEventSource converts one Chat Completions SSE stream into canonical
// events. Recv is not safe for concurrent use.
type chatEventSource struct {
	body        io.ReadCloser
	scanner     *bufio.Scanner
	accumulator *chat.Accumulator
	events      []canonical.Event
	done        bool
	sawFinish   bool
	closed      bool
}

func newChatEventSource(body io.ReadCloser, responseID canonical.ResponseID, publicModel string, ids chat.IDGenerator, names chat.ToolNameMap) *chatEventSource {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxChatSSELine)
	return &chatEventSource{
		body:        body,
		scanner:     scanner,
		accumulator: chat.NewAccumulator(responseID, publicModel, ids, names),
	}
}

func (s *chatEventSource) Recv(ctx context.Context) (canonical.Event, error) {
	if s == nil || s.closed {
		return canonical.Event{}, errors.New("chat event source is closed")
	}
	if err := ctx.Err(); err != nil {
		return canonical.Event{}, err
	}
	for len(s.events) == 0 && !s.done {
		if !s.scanner.Scan() {
			if err := s.scanner.Err(); err != nil {
				return canonical.Event{}, err
			}
			// Upstream closed the connection. A stream that reached a finish
			// reason completes normally even without [DONE]; anything else is
			// interrupted, not completed.
			if !s.sawFinish {
				return canonical.Event{}, errors.New("chat stream ended before a finish reason")
			}
			s.done = true
			finalEvents, err := s.accumulator.Finish()
			if err != nil {
				return canonical.Event{}, err
			}
			s.events = append(s.events, finalEvents...)
			break
		}
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			s.done = true
			if !s.sawFinish {
				return canonical.Event{}, errors.New("chat stream completed without a finish reason")
			}
			finalEvents, err := s.accumulator.Finish()
			if err != nil {
				return canonical.Event{}, err
			}
			s.events = append(s.events, finalEvents...)
			break
		}
		chunk, err := chat.DecodeChunk([]byte(payload))
		if err != nil {
			return canonical.Event{}, err
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				s.sawFinish = true
			}
		}
		events, err := s.accumulator.Accept(chunk)
		if err != nil {
			return canonical.Event{}, err
		}
		s.events = append(s.events, events...)
	}
	if len(s.events) == 0 {
		return canonical.Event{}, io.EOF
	}
	next := s.events[0]
	s.events = s.events[1:]
	return next, nil
}

func (s *chatEventSource) Close() error {
	if s == nil || s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}

var _ streaming.EventSource = (*chatEventSource)(nil)

// executeChat performs one retry-free chat stream attempt and returns the
// canonical event source.
func (p *Provider) executeChat(ctx context.Context, upstreamModel string, request canonical.Request) (streaming.EventSource, error) {
	body, names, err := chat.EncodeRequestWithTools(request, upstreamModel, true)
	if err != nil {
		if chat.IsUnsupported(err) {
			return nil, execution.NewError(execution.CodeInvalidRequest, "request", 0, err)
		}
		return nil, execution.NewError(execution.CodeInternal, "request", 0, err)
	}
	secret, err := p.credentials.Resolve(ctx, p.credential)
	if err != nil {
		return nil, normalizeCredentialError(err)
	}
	response, err := p.chat.Start(ctx, secret, body)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, execution.NewError(execution.CodeUnavailable, "provider.chat", retryAfterOf(0), err)
	}
	if response.StatusCode != http.StatusOK {
		defer func() { _ = response.Body.Close() }()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil, execution.NewError(codeForChatStatus(response.StatusCode), "provider.chat", retryAfterOf(parseRetryAfter(response.Header.Get("Retry-After"), clockNow())), nil)
	}
	responseID, err := p.ids.ResponseID()
	if err != nil {
		_ = response.Body.Close()
		return nil, execution.NewError(execution.CodeInternal, "provider.chat.id", 0, err)
	}
	return newChatEventSource(response.Body, responseID, request.Model(), chatAdapterIDs{ids: p.ids}, names), nil
}

func retryAfterOf(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	return delay
}

func clockNow() time.Time { return time.Now() }

// chatAdapterIDs adapts the provider's stream ID generator to the chat
// package's generator interface.
type chatAdapterIDs struct{ ids streamIDGenerator }

func (a chatAdapterIDs) ItemID() (canonical.ItemID, error) { return a.ids.ItemID() }
func (a chatAdapterIDs) ToolCallID() (canonical.ToolCallID, error) {
	return a.ids.ToolCallID()
}
func (a chatAdapterIDs) EventID() (canonical.EventID, error) { return a.ids.EventID() }

var _ chat.IDGenerator = chatAdapterIDs{}
