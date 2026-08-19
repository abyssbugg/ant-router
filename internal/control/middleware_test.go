package control

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestIDMiddleware(t *testing.T) {
	generatorCalls := 0
	generator := func() (string, error) {
		generatorCalls++
		return "generated-123", nil
	}
	handler := RequestIDs(generator)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get(RequestIDHeader) == "" {
			t.Error("request header was not populated")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(RequestIDHeader, "client-123")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Header().Get(RequestIDHeader) != "client-123" || generatorCalls != 0 {
		t.Errorf("valid client ID was replaced")
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(RequestIDHeader, "bad\nvalue")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Header().Get(RequestIDHeader) != "generated-123" || generatorCalls != 1 {
		t.Errorf("invalid client ID was not replaced")
	}
}

func TestRequestIDGenerationFailure(t *testing.T) {
	handler := RequestIDs(func() (string, error) { return "", errors.New("random failed") })(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("next handler was called")
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "internal error") {
		t.Errorf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestRecoverAndSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(RecoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("synthetic panic marker")
	})))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(WithRequestID(request.Context(), "request-123"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "synthetic panic marker") {
		t.Errorf("panic response = %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("security headers missing: %#v", recorder.Header())
	}
}

func TestRejectBodies(t *testing.T) {
	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	handler := RejectBodies(next)
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "none", want: http.StatusNoContent},
		{name: "small", body: "x", want: http.StatusBadRequest},
		{name: "large", body: strings.Repeat("x", int(MaxRequestBody)+1), want: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", strings.NewReader(test.body))
			request = request.WithContext(WithRequestID(request.Context(), "request-123"))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Errorf("status = %d, want %d", recorder.Code, test.want)
			}
		})
	}
}

func TestRequestTimeout(t *testing.T) {
	handler := RequestTimeout(10 * time.Millisecond)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
		WriteError(writer, http.StatusGatewayTimeout, CodeRequestTimeout, RequestIDFromContext(request.Context()))
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(WithRequestID(request.Context(), "request-123"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusGatewayTimeout)
	}
}
