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

// Endpoint selects the upstream dialect for one provider instance.
type Endpoint struct {
	// BaseURL overrides the official API origin; empty uses the official one.
	BaseURL string
	// Chat selects the Chat Completions wire for compatible endpoints.
	Chat bool
}

// Provider executes canonical requests through the OpenAI Responses API or,
// for compatible endpoints, the Chat Completions wire. It is safe for
// concurrent use when its credential source is safe for concurrent use.
type Provider struct {
	providerID  string
	credentials credential.Source
	credential  credential.Ref
	starter     streamStarter
	chat        chatStarter
	ids         streamIDGenerator
}

// NewProvider constructs a production OpenAI provider using the official endpoint,
// secure HTTP transport, request-time credential resolution, and zero SDK retries.
func NewProvider(source credential.Source, ref credential.Ref) (*Provider, error) {
	return NewProviderForID("openai", source, ref)
}

// NewProviderForID constructs a production provider bound to one configured
// provider identity for opaque continuation affinity.
func NewProviderForID(providerID string, source credential.Source, ref credential.Ref) (*Provider, error) {
	return newProviderWithID(providerID, source, ref, &sdkStreamStarter{factory: newProductionClientFactory()}, randomStreamIDs{})
}

func newProvider(source credential.Source, ref credential.Ref, starter streamStarter, ids streamIDGenerator) (*Provider, error) {
	return newProviderWithID("openai-test", source, ref, starter, ids)
}

// NewProviderForIDWithEndpoint constructs a provider for one configured
// endpoint. Chat-wire endpoints speak the Chat Completions dialect; responses
// endpoints use the official Responses API.
func NewProviderForIDWithEndpoint(providerID string, source credential.Source, ref credential.Ref, endpoint Endpoint) (*Provider, error) {
	if nilInterface(source) || !ref.Valid() {
		return nil, execution.NewError(execution.CodeInternal, "provider.openai.options", 0, nil)
	}
	baseURL := endpoint.BaseURL
	if baseURL == "" {
		baseURL = officialBaseURL
	}
	factory, err := newClientFactory(baseURL, newSecureHTTPClient())
	if err != nil {
		return nil, execution.NewError(execution.CodeInternal, "provider.openai.endpoint", 0, err)
	}
	provider := &Provider{providerID: providerID, credentials: source, credential: ref, starter: &sdkStreamStarter{factory: factory}, ids: randomStreamIDs{}}
	if endpoint.Chat {
		provider.chat = &httpChatStarter{factory: factory}
	}
	return provider, nil
}

func newProviderWithID(providerID string, source credential.Source, ref credential.Ref, starter streamStarter, ids streamIDGenerator) (*Provider, error) {
	if providerID == "" || nilInterface(source) || !ref.Valid() || nilInterface(starter) || nilInterface(ids) {
		return nil, execution.NewError(execution.CodeInternal, "provider.openai.options", 0, nil)
	}
	return &Provider{providerID: providerID, credentials: source, credential: ref, starter: starter, ids: ids}, nil
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
	if p.chat != nil {
		return p.executeChat(ctx, upstreamModel, request)
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
	source, err := newEventSourceWithAffinity(stream, request.Model(), p.providerID, upstreamModel, request.Output().Format(), p.ids)
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
