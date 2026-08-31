package openai

import (
	"context"
	"errors"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
	sdkresponses "github.com/openai/openai-go/v3/responses"
)

type streamStarter interface {
	Start(context.Context, credential.Secret, sdkresponses.ResponseNewParams) (sdkEventStream, error)
}

type sdkStreamStarter struct {
	factory *clientFactory
}

func (s *sdkStreamStarter) Start(ctx context.Context, secret credential.Secret, params sdkresponses.ResponseNewParams) (sdkEventStream, error) {
	if s == nil || s.factory == nil {
		return nil, errors.New("invalid OpenAI stream starter")
	}
	client := s.factory.Client(secret)
	stream := client.Responses.NewStreaming(ctx, params)
	if stream == nil {
		return nil, errors.New("OpenAI SDK returned no stream")
	}
	return stream, nil
}

// Provider executes canonical requests through the official OpenAI Responses API.
// It is safe for concurrent use when its credential source is safe for concurrent use.
type Provider struct {
	credentials credential.Source
	credential  credential.Ref
	starter     streamStarter
	ids         streamIDGenerator
}

// NewProvider constructs a production OpenAI provider using the official endpoint,
// secure HTTP transport, request-time credential resolution, and zero SDK retries.
func NewProvider(source credential.Source, ref credential.Ref) (*Provider, error) {
	return newProvider(source, ref, &sdkStreamStarter{factory: newProductionClientFactory()}, randomStreamIDs{})
}

func newProvider(source credential.Source, ref credential.Ref, starter streamStarter, ids streamIDGenerator) (*Provider, error) {
	if nilInterface(source) || !ref.Valid() || nilInterface(starter) || nilInterface(ids) {
		return nil, execution.NewError(execution.CodeInternal, "provider.openai.options", 0, nil)
	}
	return &Provider{credentials: source, credential: ref, starter: starter, ids: ids}, nil
}

// Availability resolves the configured credential without contacting OpenAI.
func (p *Provider) Availability(ctx context.Context) (execution.Availability, error) {
	if p == nil || ctx == nil || nilInterface(p.credentials) || !p.credential.Valid() {
		return execution.Availability{}, execution.NewError(execution.CodeInternal, "provider.openai", 0, nil)
	}
	_, err := p.credentials.Resolve(ctx, p.credential)
	if err == nil {
		return execution.Available(), nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return execution.Availability{}, err
	}
	if credential.CodeOf(err) == credential.CodeMissing {
		return execution.Unavailable(execution.CodeAuthenticationFailed, p.credential.Name())
	}
	return execution.Availability{}, execution.NewError(execution.CodeInternal, "provider.openai.credential", 0, err)
}

// Execute performs exactly one retry-free OpenAI stream attempt.
func (p *Provider) Execute(ctx context.Context, upstreamModel string, request canonical.Request) (streaming.EventSource, error) {
	if p == nil || ctx == nil || nilInterface(p.credentials) || nilInterface(p.starter) || nilInterface(p.ids) {
		return nil, execution.NewError(execution.CodeInternal, "provider.openai", 0, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	params, err := mapRequest(request, upstreamModel)
	if err != nil {
		return nil, err
	}
	secret, err := p.credentials.Resolve(ctx, p.credential)
	if err != nil {
		return nil, normalizeCredentialError(err)
	}
	stream, err := p.starter.Start(ctx, secret, params)
	if err != nil {
		return nil, normalizeProviderError(err)
	}
	source, err := newEventSource(stream, request.Model(), request.Output().Format(), p.ids)
	if err != nil {
		if !nilInterface(stream) {
			_ = stream.Close()
		}
		return nil, err
	}
	return source, nil
}

func normalizeCredentialError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	switch credential.CodeOf(err) {
	case credential.CodeMissing:
		return execution.NewError(execution.CodeAuthenticationFailed, "provider.openai.credential", 0, err)
	case credential.CodeInvalidReference, credential.CodeInvalidSource, credential.CodeInvalidContext:
		return execution.NewError(execution.CodeInternal, "provider.openai.credential", 0, err)
	default:
		return execution.NewError(execution.CodeInternal, "provider.openai.credential", 0, err)
	}
}

var _ execution.ProviderAdapter = (*Provider)(nil)
