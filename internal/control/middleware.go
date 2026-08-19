package control

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"time"
)

type bufferedResponse struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: make(http.Header), status: http.StatusOK}
}

func (w *bufferedResponse) Header() http.Header { return w.header }
func (w *bufferedResponse) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
}
func (w *bufferedResponse) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(data)
}
func (w *bufferedResponse) commit(writer http.ResponseWriter) {
	for key, values := range w.header {
		for _, value := range values {
			writer.Header().Add(key, value)
		}
	}
	writer.WriteHeader(w.status)
	_, _ = writer.Write(w.body.Bytes())
}

type RequestIDGenerator func() (string, error)

func RequestIDs(generator RequestIDGenerator) func(http.Handler) http.Handler {
	if generator == nil {
		generator = func() (string, error) { return GenerateRequestID(nil) }
	}
	var generatorMu sync.Mutex
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			requestID := request.Header.Get(RequestIDHeader)
			if !ValidRequestID(requestID) {
				generatorMu.Lock()
				generated, err := generator()
				generatorMu.Unlock()
				requestID = generated
				if err != nil || !ValidRequestID(requestID) {
					WriteError(writer, http.StatusInternalServerError, CodeInternalError, "")
					return
				}
			}
			request.Header.Set(RequestIDHeader, requestID)
			writer.Header().Set(RequestIDHeader, requestID)
			request = request.WithContext(WithRequestID(request.Context(), requestID))
			next.ServeHTTP(writer, request)
		})
	}
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(writer, request)
	})
}

func RecoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		buffer := newBufferedResponse()
		defer func() {
			if recover() != nil {
				_ = WriteError(writer, http.StatusInternalServerError, CodeInternalError, RequestIDFromContext(request.Context()))
				return
			}
			buffer.commit(writer)
		}()
		next.ServeHTTP(buffer, request)
	})
}

func RequestTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if timeout <= 0 {
				next.ServeHTTP(writer, request)
				return
			}
			ctx, cancel := context.WithTimeout(request.Context(), timeout)
			defer cancel()
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

// Chain applies middleware in declaration order, with the first item outermost.
func Chain(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for index := len(middleware) - 1; index >= 0; index-- {
		handler = middleware[index](handler)
	}
	return handler
}
