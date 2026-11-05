package dataplane

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/openai/chat"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// chatIDGenerator supplies canonical identifiers for the chat wire.
type chatIDGenerator struct{}

func (chatIDGenerator) ItemID() (canonical.ItemID, error) {
	return canonical.GenerateItemID(rand.Reader)
}
func (chatIDGenerator) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(rand.Reader)
}
func (chatIDGenerator) EventID() (canonical.EventID, error) {
	return canonical.GenerateEventID(rand.Reader)
}

var _ chat.IDGenerator = chatIDGenerator{}

// chatChunkWriter adapts the tracking writer + flusher to the chat sink's
// write-and-flush contract.
type chatChunkWriter struct {
	writer  *trackingWriter
	flusher http.Flusher
}

func (w *chatChunkWriter) WriteChunk(data string) error {
	written, err := w.writer.Write([]byte(data))
	if err != nil || written != len(data) {
		return errChatWriteFailed
	}
	w.flusher.Flush()
	return nil
}

var errChatWriteFailed = errors.New("chat chunk write failed")

// maxChatRequestBody matches the responses wire: MaxRequestBytes plus a
// small overhead allowance.
const maxChatRequestBody = int64(32<<20) + (64 << 10)

func readBoundedBody(body io.ReadCloser, limit int64) ([]byte, error) {
	defer func() { _ = body.Close() }()
	return io.ReadAll(io.LimitReader(body, limit+1))
}

func writeChatError(w http.ResponseWriter, err error) {
	var chatErr *chat.Error
	if !errorsAsChat(err, &chatErr) {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
		return
	}
	switch chatErr.Code {
	case "unsupported", "invalid":
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", chatErr.Code, chatErr.Path, "request is invalid")
	case "upstream_error", "protocol":
		writeAPIError(w, http.StatusBadGateway, "server_error", "upstream_error", chatErr.Path, "request failed upstream")
	default:
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
	}
}

func errorsAsChat(err error, target **chat.Error) bool {
	for err != nil {
		if chatErr, ok := err.(*chat.Error); ok {
			*target = chatErr
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// chatCompletions serves POST /v1/chat/completions: the Chat Completions
// dialect for compatible clients, translated onto the canonical protocol.
func (h *handler) chatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", "method", "method is not allowed")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_request_error", "invalid_content_type", "content_type", "content type must be application/json")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.requestTimeout)
	defer cancel()
	select {
	case h.semaphore <- struct{}{}:
		defer func() { <-h.semaphore }()
	case <-ctx.Done():
		writeAPIError(w, http.StatusRequestTimeout, "server_error", "request_timeout", "request", "request timed out")
		return
	}
	executor := h.executor
	if h.runtime != nil {
		runtime := h.runtime()
		if nilInterface(runtime) {
			writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
			return
		}
		executor = runtime
	}
	requestID, err := randomRequestIDs{}.RequestID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
		return
	}
	// The chat wire accepts the same body ceiling as the responses wire; the
	// decoder bounds its own parse, but the read is capped here first.
	body, err := readBoundedBody(r.Body, maxChatRequestBody)
	if err != nil {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "body_too_large", "request", "request body is too large")
		return
	}
	if int64(len(body)) > maxChatRequestBody {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "body_too_large", "request", "request body is too large")
		return
	}
	decoded, err := chat.DecodeRequest(body, requestID, chatIDGenerator{})
	if err != nil {
		writeChatError(w, err)
		return
	}
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
		return
	}
	tracker, ok := w.(*trackingWriter)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
		return
	}
	var sink streaming.EventSink = discardSink{}
	if decoded.Stream {
		if !supportsFlush(w) {
			writeAPIError(w, http.StatusInternalServerError, "server_error", "streaming_unsupported", "stream", "streaming is unavailable")
			return
		}
		flusher := w.(http.Flusher)
		if flusher == nil {
			writeAPIError(w, http.StatusInternalServerError, "server_error", "streaming_unsupported", "stream", "streaming is unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		sink, err = chat.NewSSEStreamSink(&chatChunkWriter{writer: tracker, flusher: flusher}, decoded.Request.Model(), h.clock().Unix())
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "stream", "streaming is unavailable")
			return
		}
	}
	source, err := executor.Execute(ctx, decoded.Request)
	if err != nil {
		writeExecutionError(w, err)
		return
	}
	pump, err := streaming.NewPump(machine, source, sink, streaming.DefaultPumpOptions())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
		return
	}
	result := pump.Run(ctx)
	if result.Cause() != nil {
		if !tracker.wrote {
			writeAPIError(w, http.StatusBadGateway, "server_error", "upstream_error", "request", "request failed")
		}
		return
	}
	if decoded.Stream {
		if !tracker.wrote {
			writeAPIError(w, http.StatusBadGateway, "server_error", "upstream_error", "request", "request failed")
		}
		return
	}
	response, ok := result.Response()
	if !ok {
		writeAPIError(w, http.StatusBadGateway, "server_error", "upstream_error", "request", "request failed")
		return
	}
	encoded, err := chat.EncodeCompletion(response, h.clock().Unix())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "response", "response encoding failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if written, writeErr := w.Write(encoded); writeErr != nil || written != len(encoded) {
		return
	}
}
