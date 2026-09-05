package anthropic

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

type providerCredentialSource struct {
	value string
	err   error
	calls int
}

func (s *providerCredentialSource) Resolve(ctx context.Context, ref credential.Ref) (credential.Secret, error) {
	s.calls++
	if s.err != nil {
		return credential.Secret{}, s.err
	}
	return credential.NewEnvSource(func(string) (string, bool) { return s.value, s.value != "" }).Resolve(ctx, ref)
}

type providerStarter struct {
	stream  sdkEventStream
	err     error
	calls   int
	secrets []string
	params  []sdk.MessageNewParams
}

func (s *providerStarter) Start(_ context.Context, secret credential.Secret, params sdk.MessageNewParams) (sdkEventStream, error) {
	s.calls++
	s.secrets = append(s.secrets, secret.Reveal())
	s.params = append(s.params, params)
	return s.stream, s.err
}

func anthropicRef(t *testing.T) credential.Ref {
	t.Helper()
	ref, err := credential.NewRef(credential.SourceEnv, "ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
func anthropicTextRequest(t *testing.T) canonical.Request {
	t.Helper()
	return decodeRequest(t, `{"model":"anthropic/public","input":"hello"}`)
}

func drainAnthropicProvider(ctx context.Context, source streaming.EventSource) (canonical.Response, error) {
	defer source.Close()
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		return canonical.Response{}, err
	}
	for {
		event, err := source.Recv(ctx)
		if errors.Is(err, io.EOF) {
			return canonical.Response{}, errors.New("stream ended before response")
		}
		if err != nil {
			return canonical.Response{}, err
		}
		transition, err := machine.Accept(event)
		if err != nil {
			return canonical.Response{}, err
		}
		if response, ok := transition.Response(); ok {
			return response, nil
		}
	}
}

func TestProviderAvailabilityRotationAndSingleAttempt(t *testing.T) {
	creds := &providerCredentialSource{value: "first-private-key"}
	starter := &providerStarter{stream: &fakeSDKStream{events: textSDKEvents(t)}}
	provider, err := newProvider(creds, anthropicRef(t), 1024, 4096, starter, &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	available, err := provider.Availability(context.Background())
	if err != nil || !available.Available() || starter.calls != 0 {
		t.Fatalf("availability = %#v, err=%v, starts=%d", available, err, starter.calls)
	}
	first, err := provider.Execute(context.Background(), "claude-upstream", anthropicTextRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drainAnthropicProvider(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	creds.value = "second-private-key"
	starter.stream = &fakeSDKStream{events: textSDKEvents(t)}
	second, err := provider.Execute(context.Background(), "claude-upstream", anthropicTextRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drainAnthropicProvider(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if starter.calls != 2 || starter.secrets[0] != "first-private-key" || starter.secrets[1] != "second-private-key" || starter.params[0].Model != "claude-upstream" || starter.params[0].MaxTokens != 1024 {
		t.Fatalf("starter = %#v", starter)
	}
}

func TestProviderMapsBeforeCredentialAndReportsMissing(t *testing.T) {
	creds := &providerCredentialSource{}
	starter := &providerStarter{}
	provider, err := newProvider(creds, anthropicRef(t), 1024, 4096, starter, &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Execute(context.Background(), "", anthropicTextRequest(t))
	if execution.CodeOf(err) != execution.CodeInvalidRequest || creds.calls != 0 || starter.calls != 0 {
		t.Fatalf("invalid execute = %v, credentials=%d, starts=%d", err, creds.calls, starter.calls)
	}
	availability, err := provider.Availability(context.Background())
	if err != nil || availability.Available() || availability.Code() != execution.CodeAuthenticationFailed || availability.Reference() != "ANTHROPIC_API_KEY" {
		t.Fatalf("availability = %#v, %v", availability, err)
	}
	_, err = provider.Execute(context.Background(), "claude", anthropicTextRequest(t))
	if execution.CodeOf(err) != execution.CodeAuthenticationFailed || strings.Contains(err.Error(), "ANTHROPIC_API_KEY") || starter.calls != 0 {
		t.Fatalf("missing credential = %v", err)
	}
}

func TestProviderClassifiesCredentialErrorsAndRedactsSecret(t *testing.T) {
	ref := anthropicRef(t)
	invalid := &providerCredentialSource{err: execution.NewError(execution.CodeInternal, "credential", 0, errors.New("source failure"))}
	provider, err := newProvider(invalid, ref, 1024, 4096, &providerStarter{}, &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Execute(context.Background(), "claude", anthropicTextRequest(t)); execution.CodeOf(err) != execution.CodeInternal {
		t.Fatalf("invalid credential source = %v", err)
	}

	const secret = "actual-private-api-key"
	creds := &providerCredentialSource{value: secret}
	starter := &providerStarter{err: errors.New(secret)}
	provider, err = newProvider(creds, ref, 1024, 4096, starter, &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Execute(context.Background(), "claude", anthropicTextRequest(t))
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("provider error leaked secret: %v", err)
	}
}

func TestProviderNormalizesStartupAndCancellation(t *testing.T) {
	creds := &providerCredentialSource{value: "private"}
	starter := &providerStarter{err: &net.DNSError{Err: "private marker", Name: "secret.example"}}
	provider, _ := newProvider(creds, anthropicRef(t), 1024, 4096, starter, &sourceIDs{})
	_, err := provider.Execute(context.Background(), "claude", anthropicTextRequest(t))
	if execution.CodeOf(err) != execution.CodeUnavailable || strings.Contains(err.Error(), "private") || starter.calls != 1 {
		t.Fatalf("startup = %v, calls=%d", err, starter.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = provider.Execute(ctx, "claude", anthropicTextRequest(t))
	if !errors.Is(err, context.Canceled) || starter.calls != 1 {
		t.Fatalf("cancel = %v, calls=%d", err, starter.calls)
	}
}

func TestProviderRejectsInvalidConstruction(t *testing.T) {
	var nilSource *providerCredentialSource
	var nilStarter *providerStarter
	cases := []struct {
		source   credential.Source
		starter  streamStarter
		ids      streamIDGenerator
		def, max int64
	}{
		{nil, &providerStarter{}, &sourceIDs{}, 1, 1}, {nilSource, &providerStarter{}, &sourceIDs{}, 1, 1},
		{credential.NewEnvSource(nil), nil, &sourceIDs{}, 1, 1}, {credential.NewEnvSource(nil), nilStarter, &sourceIDs{}, 1, 1},
		{credential.NewEnvSource(nil), &providerStarter{}, nil, 1, 1}, {credential.NewEnvSource(nil), &providerStarter{}, &sourceIDs{}, 0, 1},
	}
	for index, test := range cases {
		if provider, err := newProvider(test.source, anthropicRef(t), test.def, test.max, test.starter, test.ids); provider != nil || execution.CodeOf(err) != execution.CodeInternal {
			t.Fatalf("case %d = %#v, %v", index, provider, err)
		}
	}
}

var _ execution.ProviderAdapter = (*Provider)(nil)
