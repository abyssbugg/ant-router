//go:build phase4bstress

package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	inbound "github.com/abyssbugg/ant-router/internal/openai/responses"
)

const (
	continuationStressWorkers = 64
	continuationStressRounds  = 8
)

func TestOpenAIContinuationReplayConcurrentStress(t *testing.T) {
	wire := []byte(`{"model":"openai/public","store":false,"include":["reasoning.encrypted_content"],"input":[{"type":"reasoning","id":"rs_upstream","encrypted_content":"cipher-stress","summary":[{"type":"summary_text","text":"use tool"}]},{"type":"function_call","id":"fc_upstream","call_id":"call_upstream","name":"Search","arguments":"{\"query\":\"docs\"}","status":"completed"},{"type":"function_call_output","call_id":"call_upstream","output":"done"}],"tools":[{"type":"function","name":"Search","parameters":{"type":"object"},"strict":true}]}`)
	parsed, err := inbound.DecodeWithAffinity(bytes.NewReader(wire), &mapperIDs{}, mapperAffinityResolver{providerID: "openai-primary", upstreamModel: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}

	errors := make(chan error, continuationStressWorkers)
	var wait sync.WaitGroup
	for worker := 0; worker < continuationStressWorkers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for round := 0; round < continuationStressRounds; round++ {
				params, err := mapRequest(parsed.Request, "gpt-test")
				if err != nil {
					errors <- err
					return
				}
				encoded, err := json.Marshal(params)
				if err != nil {
					errors <- err
					return
				}
				for _, expected := range [][]byte{[]byte(`"id":"rs_upstream"`), []byte(`"encrypted_content":"cipher-stress"`), []byte(`"id":"fc_upstream"`), []byte(`"call_id":"call_upstream"`)} {
					if !bytes.Contains(encoded, expected) {
						errors <- fmt.Errorf("mapped request omitted replay identity")
						return
					}
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}
