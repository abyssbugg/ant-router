//go:build canonicalstress

package canonical

import (
	"bytes"
	"sync"
	"testing"
)

func TestCanonicalConcurrentReadStress(t *testing.T) {
	request := codecRequest(t, 1)
	response := codecResponse(t)
	events := codecEvents(t)
	expectedRequest, _ := EncodeRequest(request)
	expectedResponse, _ := EncodeResponse(response)

	const workers = 128
	const iterations = 100
	errorsFound := make(chan string, 1)
	var wait sync.WaitGroup
	wait.Add(workers)
	for range workers {
		go func() {
			defer wait.Done()
			for range iterations {
				encodedRequest, err := EncodeRequest(request)
				if err != nil || !bytes.Equal(encodedRequest, expectedRequest) || !request.Valid() {
					reportStress(errorsFound, "request changed")
					return
				}
				encodedResponse, err := EncodeResponse(response)
				if err != nil || !bytes.Equal(encodedResponse, expectedResponse) || !response.Valid() {
					reportStress(errorsFound, "response changed")
					return
				}
				for _, event := range events {
					if _, err := EncodeEvent(event); err != nil || !event.Valid() {
						reportStress(errorsFound, "event changed")
						return
					}
				}
				copyInput := request.Input()
				copyInput[0] = Item{}
				if !request.Input()[0].Valid() {
					reportStress(errorsFound, "request copy mutated")
					return
				}
			}
		}()
	}
	wait.Wait()
	select {
	case message := <-errorsFound:
		t.Fatal(message)
	default:
	}
}

func reportStress(target chan<- string, message string) {
	select {
	case target <- message:
	default:
	}
}
