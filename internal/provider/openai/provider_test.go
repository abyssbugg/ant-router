package openai

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
	sdkresponses "github.com/openai/openai-go/v3/responses"
)

type recordingCredentialSource struct {
	value string
	err   error
	calls int
}

func (s *recordingCredentialSource) Resolve(ctx context.Context, ref credential.Ref) (credential.Secret, error) {
	s.calls++
	if s.err != nil {
		return credential.Secret{}, s.err
	}
	env := credential.NewEnvSource(func(name string) (string, bool) { return s.value, true })
	return env.Resolve(ctx, ref)
}

type starterCall struct {
	secret string
	params sdkresponses.ResponseNewParams
}

type recordingStreamStarter struct {
	stream sdkEventStream
	err    error
	calls  []starterCall
}

func (s *recordingStreamStarter) Start(_ context.Context, secret credential.Secret, params sdkresponses.ResponseNewParams) (sdkEventStream, error) {
	s.calls = append(s.calls, starterCall{secret: secret.Reveal(), params: params})
	return s.stream, s.err
}

func openAIRef(t *testing.T) credential.Ref {
	t.Helper()
	ref, err := credential.NewRef(credential.SourceEnv, "OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func newFakeSDKStream(events []sdkresponses.ResponseStreamEventUnion) *fakeSDKStream {
	return &fakeSDKStream{events: events}
}

func deterministicSourceIDs(*testing.T) *sourceIDs { return &sourceIDs{} }

func textRequest(t *testing.T) canonical.Request {
	t.Helper()
	return decodeCanonicalRequest(t, `{"model":"openai/public","input":"hello"}`)
}

func drainProviderSource(ctx context.Context, source streaming.EventSource) (canonical.Response, error) {
	defer source.Close()
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		return canonical.Response{}, err
	}
	for {
		event, receiveErr := source.Recv(ctx)
		if errors.Is(receiveErr, io.EOF) {
			return canonical.Response{}, errors.New("provider source ended before a response")
		}
		if receiveErr != nil {
			return canonical.Response{}, receiveErr
		}
		transition, acceptErr := machine.Accept(event)
		if acceptErr != nil {
			return canonical.Response{}, acceptErr
		}
		if response, ok := transition.Response(); ok {
			return response, nil
		}
	}
}

func TestProviderAvailabilityAndCredentialRotation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ref := openAIRef(t)
	source := &recordingCredentialSource{value: "first-private-key"}
	starter := &recordingStreamStarter{stream: newFakeSDKStream(textSDKEvents(t))}
	provider, err := newProvider(source, ref, starter, deterministicSourceIDs(t))
	if err != nil {
		t.Fatal(err)
	}

	availability, err := provider.Availability(ctx)
	if err != nil || !availability.Available() {
		t.Fatalf("availability = %#v, %v", availability, err)
	}
	if len(starter.calls) != 0 {
		t.Fatal("availability contacted the provider")
	}

	request := textRequest(t)
	first, err := provider.Execute(ctx, "gpt-test", request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drainProviderSource(ctx, first); err != nil {
		t.Fatal(err)
	}
	source.value = "second-private-key"
	starter.stream = newFakeSDKStream(textSDKEvents(t))
	second, err := provider.Execute(ctx, "gpt-test", request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drainProviderSource(ctx, second); err != nil {
		t.Fatal(err)
	}
	if source.calls != 3 {
		t.Fatalf("credential resolves = %d, want 3", source.calls)
	}
	if len(starter.calls) != 2 || starter.calls[0].secret != "first-private-key" || starter.calls[1].secret != "second-private-key" {
		t.Fatalf("starter calls = %#v", starter.calls)
	}
	if starter.calls[0].params.Model != "gpt-test" || starter.calls[1].params.Model != "gpt-test" {
		t.Fatal("upstream model was not mapped")
	}
}

func TestProviderMissingCredentialIsUnavailable(t *testing.T) {
	t.Parallel()
	ref := openAIRef(t)
	source := credential.NewEnvSource(func(string) (string, bool) { return "", false })
	starter := &recordingStreamStarter{}
	provider, err := newProvider(source, ref, starter, deterministicSourceIDs(t))
	if err != nil {
		t.Fatal(err)
	}

	availability, err := provider.Availability(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if availability.Available() || availability.Code() != execution.CodeAuthenticationFailed || availability.Reference() != "OPENAI_API_KEY" {
		t.Fatalf("availability = %#v", availability)
	}
	_, err = provider.Execute(context.Background(), "gpt-test", textRequest(t))
	if execution.CodeOf(err) != execution.CodeAuthenticationFailed {
		t.Fatalf("execute error = %v", err)
	}
	if len(starter.calls) != 0 || containsText(err.Error(), "OPENAI_API_KEY") {
		t.Fatal("missing credential leaked or contacted provider")
	}
}

func TestProviderMapsBeforeStartingAndAttemptsOnce(t *testing.T) {
	t.Parallel()
	ref := openAIRef(t)
	source := &recordingCredentialSource{value: "private-key"}
	starter := &recordingStreamStarter{stream: newFakeSDKStream(textSDKEvents(t))}
	provider, err := newProvider(source, ref, starter, deterministicSourceIDs(t))
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Execute(context.Background(), "", textRequest(t))
	if execution.CodeOf(err) != execution.CodeInvalidRequest || len(starter.calls) != 0 {
		t.Fatalf("invalid execution = %v, calls=%d", err, len(starter.calls))
	}
	if source.calls != 0 {
		t.Fatal("credential resolved before request mapping")
	}

	_, err = provider.Execute(context.Background(), "gpt-test", textRequest(t))
	if err != nil || len(starter.calls) != 1 {
		t.Fatalf("execution = %v, calls=%d", err, len(starter.calls))
	}
}

func TestProviderNormalizesStarterAndCancellationErrors(t *testing.T) {
	t.Parallel()
	ref := openAIRef(t)
	t.Run("network startup", func(t *testing.T) {
		source := &recordingCredentialSource{value: "private-key"}
		starter := &recordingStreamStarter{err: &net.DNSError{Err: "private network canary", Name: "private.example"}}
		provider, err := newProvider(source, ref, starter, deterministicSourceIDs(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = provider.Execute(context.Background(), "gpt-test", textRequest(t))
		if execution.CodeOf(err) != execution.CodeUnavailable || containsText(err.Error(), "private") {
			t.Fatalf("starter error = %v", err)
		}
		if len(starter.calls) != 1 {
			t.Fatalf("starter attempts = %d", len(starter.calls))
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		source := &recordingCredentialSource{value: "private-key"}
		starter := &recordingStreamStarter{}
		provider, err := newProvider(source, ref, starter, deterministicSourceIDs(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = provider.Execute(ctx, "gpt-test", textRequest(t))
		if !errors.Is(err, context.Canceled) || len(starter.calls) != 0 {
			t.Fatalf("cancelled error = %v, calls=%d", err, len(starter.calls))
		}
	})
}

func TestProviderRejectsInvalidConstruction(t *testing.T) {
	t.Parallel()
	ref := openAIRef(t)
	var nilSource *recordingCredentialSource
	var nilStarter *recordingStreamStarter
	cases := []struct {
		name    string
		source  credential.Source
		ref     credential.Ref
		starter streamStarter
		ids     streamIDGenerator
	}{
		{name: "nil source", source: nil, ref: ref, starter: &recordingStreamStarter{}, ids: deterministicSourceIDs(t)},
		{name: "typed nil source", source: nilSource, ref: ref, starter: &recordingStreamStarter{}, ids: deterministicSourceIDs(t)},
		{name: "invalid reference", source: credential.NewEnvSource(nil), starter: &recordingStreamStarter{}, ids: deterministicSourceIDs(t)},
		{name: "nil starter", source: credential.NewEnvSource(nil), ref: ref, ids: deterministicSourceIDs(t)},
		{name: "typed nil starter", source: credential.NewEnvSource(nil), ref: ref, starter: nilStarter, ids: deterministicSourceIDs(t)},
		{name: "nil ids", source: credential.NewEnvSource(nil), ref: ref, starter: &recordingStreamStarter{}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			provider, err := newProvider(test.source, test.ref, test.starter, test.ids)
			if provider != nil || execution.CodeOf(err) != execution.CodeInternal {
				t.Fatalf("newProvider() = %#v, %v", provider, err)
			}
		})
	}
}

var _ execution.ProviderAdapter = (*Provider)(nil)
