package dataplane

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInvalidImageFailsBeforeSSECommitAndRedactsInput(t *testing.T) {
	handler, token := testHandler(t)
	cases := []struct {
		name string
		body string
	}{
		{"invalid base64", `{"model":"mock/vision","stream":true,"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,PRIVATE_IMAGE_MARKER%%%"}]}]}`},
		{"HTTP URL", `{"model":"mock/vision","stream":true,"input":[{"role":"user","content":[{"type":"input_image","image_url":"http://private.example/image"}]}]}`},
		{"file ID", `{"model":"mock/vision","stream":true,"input":[{"role":"user","content":[{"type":"input_image","file_id":"file-PRIVATE_IMAGE_MARKER"}]}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			request := httptest.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8787/v1/responses", strings.NewReader(tc.body))
			request.Host = "127.0.0.1:8787"
			request.Header.Set("Authorization", "Bearer "+token.String())
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
			}
			if !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") || bytes.Contains(recorder.Body.Bytes(), []byte("data: ")) || bytes.Contains(recorder.Body.Bytes(), []byte("PRIVATE_IMAGE_MARKER")) || bytes.Contains(recorder.Body.Bytes(), []byte("private.example")) || bytes.Contains(recorder.Body.Bytes(), []byte(token.String())) {
				t.Fatalf("unsafe precommit response headers=%v body=%q", recorder.Header(), recorder.Body.String())
			}
		})
	}
}
