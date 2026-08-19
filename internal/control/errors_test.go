package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorCodesAndSafeMessages(t *testing.T) {
	codes := []ErrorCode{CodeUnauthorized, CodeInvalidRequest, CodeNotFound, CodeMethodNotAllowed, CodeBodyTooLarge, CodeUnsupportedMediaType, CodeRequestTimeout, CodeServiceUnavailable, CodeInternalError}
	for _, code := range codes {
		if code == "" || safeMessage(code) == "" {
			t.Errorf("code %q has no safe message", code)
		}
	}
}

func TestWriteErrorEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := WriteError(recorder, http.StatusUnauthorized, CodeUnauthorized, "request-123"); err != nil {
		t.Fatalf("WriteError() failed: %v", err)
	}
	response := recorder.Result()
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("Body.Close() failed: %v", err)
		}
	}()
	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d", response.StatusCode)
	}
	if response.Header.Get("Content-Type") != JSONContentType || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("security headers = %#v", response.Header)
	}
	var envelope ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() failed: %v", err)
	}
	if envelope.Error.Code != CodeUnauthorized || envelope.Error.Message != "authorization failed" || envelope.Error.RequestID != "request-123" {
		t.Errorf("envelope = %#v", envelope)
	}
}

func TestWriteErrorNeverAcceptsInternalMessage(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteError(recorder, http.StatusInternalServerError, CodeInternalError, "synthetic-secret-marker")
	if strings.Contains(recorder.Body.String(), "panic value") {
		t.Error("error envelope exposed internal details")
	}
}
