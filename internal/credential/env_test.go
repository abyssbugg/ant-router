package credential

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func mustEnvRef(t *testing.T) Ref {
	t.Helper()
	ref, err := NewRef(SourceEnv, "OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestEnvSourceResolvesEveryCallAndObservesRotation(t *testing.T) {
	var value atomic.Value
	value.Store("first-provider-key")
	source := NewEnvSource(func(name string) (string, bool) {
		if name != "OPENAI_API_KEY" {
			t.Fatalf("lookup name = %q", name)
		}
		return value.Load().(string), true
	})

	first, err := source.Resolve(context.Background(), mustEnvRef(t))
	if err != nil || first.Reveal() != "first-provider-key" {
		t.Fatalf("first resolve = %q, %v", first.Reveal(), err)
	}
	value.Store("second-provider-key")
	second, err := source.Resolve(context.Background(), mustEnvRef(t))
	if err != nil || second.Reveal() != "second-provider-key" {
		t.Fatalf("second resolve = %q, %v", second.Reveal(), err)
	}
}

func TestEnvSourceRejectsMissingAndBlankValues(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		found bool
	}{
		{"missing", "", false},
		{"empty", "", true},
		{"spaces", "  \t\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := NewEnvSource(func(string) (string, bool) { return test.value, test.found })
			_, err := source.Resolve(context.Background(), mustEnvRef(t))
			if CodeOf(err) != CodeMissing {
				t.Fatalf("Resolve() code = %q, want %q", CodeOf(err), CodeMissing)
			}
			if err != nil && test.value != "" && strings.Contains(err.Error(), test.value) {
				t.Fatal("Resolve() error exposed credential value")
			}
		})
	}
}

func TestEnvSourcePreservesCancellationWithoutLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	source := NewEnvSource(func(string) (string, bool) {
		calls.Add(1)
		return "never-print-this-provider-key", true
	})
	_, err := source.Resolve(ctx, mustEnvRef(t))
	if CodeOf(err) != CodeCancelled || !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("Resolve() = %v, calls = %d", err, calls.Load())
	}
}

func TestEnvSourcePreservesCancellationAfterLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := NewEnvSource(func(string) (string, bool) {
		cancel()
		return "never-print-this-provider-key", true
	})
	_, err := source.Resolve(ctx, mustEnvRef(t))
	if CodeOf(err) != CodeCancelled || !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve() = %v", err)
	}
}

func TestEnvSourceRejectsInvalidInputsAndTypedNil(t *testing.T) {
	var source *EnvSource
	if _, err := source.Resolve(context.Background(), mustEnvRef(t)); CodeOf(err) != CodeInvalidSource {
		t.Fatalf("typed nil source code = %q", CodeOf(err))
	}
	valid := NewEnvSource(func(string) (string, bool) { return "key", true })
	if _, err := valid.Resolve(nil, mustEnvRef(t)); CodeOf(err) != CodeInvalidContext {
		t.Fatalf("nil context code = %q", CodeOf(err))
	}
	if _, err := valid.Resolve(context.Background(), Ref{}); CodeOf(err) != CodeInvalidReference {
		t.Fatalf("invalid reference code = %q", CodeOf(err))
	}
}
