//go:build phase3bstress

package dataplane

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestImageInferenceConcurrentStress(t *testing.T) {
	handler, token := testHandler(t)
	png := make([]byte, 24)
	copy(png, []byte("\x89PNG\r\n\x1a\n"))
	binary.BigEndian.PutUint32(png[8:12], 13)
	copy(png[12:16], "IHDR")
	binary.BigEndian.PutUint32(png[16:20], 64)
	binary.BigEndian.PutUint32(png[20:24], 32)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)

	const workers = 64
	const rounds = 8
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for round := 0; round < rounds; round++ {
				stream := (worker+round)%2 == 0
				source := dataURL
				if (worker+round)%3 == 0 {
					source = "https://example.com/image"
				}
				body := fmt.Sprintf(`{"model":"mock/vision","stream":%t,"input":[{"role":"user","content":[{"type":"input_text","text":"inspect"},{"type":"input_image","image_url":%q,"detail":"auto"}]}]}`, stream, source)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				request := httptest.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8787/v1/responses", strings.NewReader(body))
				request.Host = "127.0.0.1:8787"
				request.Header.Set("Authorization", "Bearer "+token.String())
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, request)
				cancel()
				if recorder.Code != http.StatusOK {
					errors <- fmt.Errorf("worker=%d round=%d status=%d", worker, round, recorder.Code)
					return
				}
				if !strings.Contains(recorder.Body.String(), "images=1") {
					errors <- fmt.Errorf("worker=%d round=%d missing vision summary", worker, round)
					return
				}
			}
		}(worker)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}
