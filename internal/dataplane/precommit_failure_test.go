package dataplane

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	"github.com/abyssbugg/ant-router/internal/provider/mock"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

type scriptedEventSource struct {
	events []canonical.Event
	index  int
}

func (s *scriptedEventSource) Recv(context.Context) (canonical.Event, error) {
	if s.index < len(s.events) {
		event := s.events[s.index]
		s.index++
		return event, nil
	}
	return canonical.Event{}, io.EOF
}

func (s *scriptedEventSource) Close() error { return nil }

type scriptedExecutor struct {
	source streaming.EventSource
}

func (e *scriptedExecutor) Execute(context.Context, canonical.Request) (streaming.EventSource, error) {
	return e.source, nil
}

func newPrecommitFailureHandler(t *testing.T) (http.Handler, inferencetoken.Token) {
	t.Helper()
	token := inferencetoken.Token{}
	for index := range token {
		token[index] = 7
	}
	provider, err := mock.NewRandom(time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	responseID, err := canonical.GenerateResponseID(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	startedID, err := canonical.GenerateEventID(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	started, err := canonical.NewResponseStartedEvent(startedID, responseID, 0, "mock/text")
	if err != nil {
		t.Fatal(err)
	}
	failure, err := canonical.NewFailure(canonical.FailureParams{Code: canonical.FailureCode("upstream_protocol_error"), Message: "upstream refused"})
	if err != nil {
		t.Fatal(err)
	}
	failedID, err := canonical.GenerateEventID(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := canonical.NewResponseFailedEvent(failedID, responseID, 1, failure, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(HandlerOptions{
		TokenSource:   StaticTokenSource(token),
		Executor:      &scriptedExecutor{source: &scriptedEventSource{events: []canonical.Event{started, failed}}},
		Catalog:       provider,
		ListenAddress: "127.0.0.1:8787", MaxConcurrent: 4, RequestTimeout: time.Second,
		Clock: func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, token
}

func TestStreamingPrecommitFailureReturnsHTTPError(t *testing.T) {
	handler, token := newPrecommitFailureHandler(t)
	recorder := serve(t, handler, token, http.MethodPost, "/v1/responses", `{"model":"mock/text","input":"hello","stream":true}`)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", recorder.Header().Get("Content-Type"))
	}
	if strings.Contains(recorder.Body.String(), "event:") || strings.Contains(recorder.Body.String(), "upstream refused") {
		t.Fatalf("response leaked precommit stream state: %q", recorder.Body.String())
	}
}
