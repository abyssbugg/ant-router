package anthropic

import (
	"context"
	"errors"
	"strings"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

type streamStarter interface {
	Start(context.Context, credential.Secret, sdk.MessageNewParams) (sdkEventStream, error)
}

type sdkStreamStarter struct{ factory *clientFactory }

func (s *sdkStreamStarter) Start(ctx context.Context, secret credential.Secret, params sdk.MessageNewParams) (sdkEventStream, error) {
	if s == nil || s.factory == nil {
		return nil, errors.New("invalid Anthropic stream starter")
	}
	client := s.factory.Client(secret)
	stream := client.Messages.NewStreaming(ctx, params)
	if stream == nil {
		return nil, errors.New("Anthropic SDK returned no stream")
	}
	return stream, nil
}

// ModelLimits defines configured output-token bounds for one upstream model.
type ModelLimits struct {
	Upstream               string
	DefaultMaxOutputTokens int64
	MaxOutputTokens        int64
}

// Options configures optional provider parameters.
type Options struct {
	// Proxy configures outbound proxy: "" for direct, "environment" for env vars,
	// or http/https/socks5 URL.
	Proxy string
}

type modelLimits struct {
	defaultMax int64
	maximum    int64
}

// Provider executes canonical requests through the official Anthropic Messages API.
// It is safe for concurrent use when its credential source is safe for concurrent use.
type Provider struct {
	credentials credential.Source
	credential  credential.Ref
	models      map[string]modelLimits
	starter     streamStarter
	ids         streamIDGenerator
}

// NewProvider constructs a retry-free production Anthropic provider using the
// official endpoint and request-time environment credential resolution.
func NewProvider(source credential.Source, ref credential.Ref, models []ModelLimits) (*Provider, error) {
	return NewProviderWithOptions(source, ref, models, Options{})
}

// NewProviderWithOptions constructs a provider with optional configuration like proxy.
func NewProviderWithOptions(source credential.Source, ref credential.Ref, models []ModelLimits, opts Options) (*Provider, error) {
	client, err := newSecureHTTPClientWithProxy(opts.Proxy)
	if err != nil {
		return nil, err
	}
	factory, err := newClientFactory(officialBaseURL, client)
	if err != nil {
		return nil, err
	}
	return newProvider(source, ref, models, &sdkStreamStarter{factory: factory}, randomStreamIDs{})
}

func newProvider(source credential.Source, ref credential.Ref, models []ModelLimits, starter streamStarter, ids streamIDGenerator) (*Provider, error) {
	if nilInterface(source) || !ref.Valid() || len(models) == 0 || nilInterface(starter) || nilInterface(ids) {
		return nil, execution.NewError(execution.CodeInternal, "provider.anthropic.options", 0, nil)
	}
	limits := make(map[string]modelLimits, len(models))
	for _, model := range models {
		upstream := strings.TrimSpace(model.Upstream)
		if upstream == "" || upstream != model.Upstream || model.DefaultMaxOutputTokens <= 0 || model.MaxOutputTokens <= 0 || model.DefaultMaxOutputTokens > model.MaxOutputTokens || model.MaxOutputTokens > int64(canonical.MaxOutputTokens) {
			return nil, execution.NewError(execution.CodeInternal, "provider.anthropic.options", 0, nil)
		}
		if _, exists := limits[upstream]; exists {
			return nil, execution.NewError(execution.CodeInternal, "provider.anthropic.options", 0, nil)
		}
		limits[upstream] = modelLimits{defaultMax: model.DefaultMaxOutputTokens, maximum: model.MaxOutputTokens}
	}
	return &Provider{credentials: source, credential: ref, models: limits, starter: starter, ids: ids}, nil
}

// Availability resolves the credential without contacting Anthropic.
func (p *Provider) Availability(ctx context.Context) (execution.Availability, error) {
	if p == nil || ctx == nil || nilInterface(p.credentials) || !p.credential.Valid() {
		return execution.Availability{}, execution.NewError(execution.CodeInternal, "provider.anthropic", 0, nil)
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
	return execution.Availability{}, execution.NewError(execution.CodeInternal, "provider.anthropic.credential", 0, err)
}

// Execute performs exactly one retry-free Anthropic streaming attempt.
func (p *Provider) Execute(ctx context.Context, upstreamModel string, request canonical.Request) (streaming.EventSource, error) {
	if p == nil || ctx == nil || nilInterface(p.credentials) || nilInterface(p.starter) || nilInterface(p.ids) {
		return nil, execution.NewError(execution.CodeInternal, "provider.anthropic", 0, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits, exists := p.models[upstreamModel]
	if !exists {
		return nil, execution.NewError(execution.CodeInvalidModel, "request.model", 0, nil)
	}
	params, err := mapRequest(request, upstreamModel, limits.defaultMax, limits.maximum)
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
	if credential.CodeOf(err) == credential.CodeMissing {
		return execution.NewError(execution.CodeAuthenticationFailed, "provider.anthropic.credential", 0, err)
	}
	return execution.NewError(execution.CodeInternal, "provider.anthropic.credential", 0, err)
}

var _ execution.ProviderAdapter = (*Provider)(nil)
