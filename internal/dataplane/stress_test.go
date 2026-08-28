//go:build phase3astress

package dataplane

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentInferenceStress(t *testing.T) {
	handler, token := testHandler(t)
	const workers = 64
	const rounds = 8
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for round := 0; round < rounds; round++ {
				stream := (worker+round)%2 == 0
				body := `{"model":"mock/text","input":"hello"}`
				if stream {
					body = `{"model":"mock/text","input":"hello","stream":true}`
				}
				request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/v1/responses", strings.NewReader(body))
				request.Header.Set("Authorization", "Bearer "+token.String())
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					errors <- fmt.Errorf("worker %d round %d stream %t status %d", worker, round, stream, response.Code)
					return
				}
			}
		}(worker)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}
