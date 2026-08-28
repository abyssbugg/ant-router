package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/controltoken"
)

const MaxResponseBody int64 = 64 << 10

type Dialer interface {
	DialContext(context.Context) (net.Conn, error)
}

type TokenSource interface {
	Token(context.Context) (controltoken.Token, error)
}

type ClientOptions struct {
	RequestTimeout   time.Duration
	MaxResponseBytes int64
	MaxIdleConns     int
	IdleConnTimeout  time.Duration
}

func DefaultClientOptions() ClientOptions {
	return ClientOptions{RequestTimeout: 3 * time.Second, MaxResponseBytes: MaxResponseBody, MaxIdleConns: 2, IdleConnTimeout: 30 * time.Second}
}

type Client struct {
	httpClient      *http.Client
	transport       *http.Transport
	tokenSource     TokenSource
	requestTimeout  time.Duration
	maxResponseBody int64
}

func NewClient(dialer Dialer, tokenSource TokenSource, options ClientOptions) (*Client, error) {
	if dialer == nil || tokenSource == nil {
		return nil, errors.New("control client dependencies are required")
	}
	if options.RequestTimeout <= 0 || options.MaxResponseBytes <= 0 || options.MaxIdleConns <= 0 || options.IdleConnTimeout <= 0 {
		return nil, errors.New("control client options are invalid")
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           func(ctx context.Context, _, _ string) (net.Conn, error) { return dialer.DialContext(ctx) },
		DisableCompression:    true,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          options.MaxIdleConns,
		MaxIdleConnsPerHost:   options.MaxIdleConns,
		IdleConnTimeout:       options.IdleConnTimeout,
		ResponseHeaderTimeout: options.RequestTimeout,
	}
	return &Client{
		httpClient:      &http.Client{Transport: transport},
		transport:       transport,
		tokenSource:     tokenSource,
		requestTimeout:  options.RequestTimeout,
		maxResponseBody: options.MaxResponseBytes,
	}, nil
}

func (c *Client) Health(ctx context.Context) (HealthResponse, error) {
	var response HealthResponse
	if err := c.request(ctx, http.MethodGet, "/v1/health", nil, &response); err != nil {
		return HealthResponse{}, err
	}
	if err := validateHealthResponse(response); err != nil {
		return HealthResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: err}
	}
	return response, nil
}

func (c *Client) Version(ctx context.Context) (VersionResponse, error) {
	var response VersionResponse
	if err := c.request(ctx, http.MethodGet, "/v1/version", nil, &response); err != nil {
		return VersionResponse{}, err
	}
	if response.APIVersion != APIVersion || response.Name != "ant-router" || response.Version == "" {
		return VersionResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: errors.New("version response is invalid")}
	}
	return response, nil
}

func (c *Client) Status(ctx context.Context) (StatusResponse, error) {
	var response StatusResponse
	if err := c.request(ctx, http.MethodGet, "/v1/status", nil, &response); err != nil {
		return StatusResponse{}, err
	}
	if err := validateStatusResponse(response); err != nil {
		return StatusResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: err}
	}
	return response, nil
}

func (c *Client) CurrentConfig(ctx context.Context) (ConfigCurrentResponse, error) {
	var response ConfigCurrentResponse
	if err := c.request(ctx, http.MethodGet, "/v1/config", nil, &response); err != nil {
		return ConfigCurrentResponse{}, err
	}
	if err := validateConfigCurrent(response); err != nil {
		return ConfigCurrentResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: err}
	}
	return response, nil
}

func (c *Client) ValidateConfig(ctx context.Context, tomlBytes []byte) (ConfigValidateResponse, error) {
	if len(tomlBytes) > config.MaxSourceBytes {
		return ConfigValidateResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: errors.New("configuration is too large")}
	}
	if !utf8.Valid(tomlBytes) {
		return ConfigValidateResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: errors.New("configuration is not valid UTF-8")}
	}
	body, err := json.Marshal(ConfigValidateRequest{TOML: string(tomlBytes)})
	if err != nil {
		return ConfigValidateResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: err}
	}
	var response ConfigValidateResponse
	if err := c.request(ctx, http.MethodPost, "/v1/config/validate", body, &response); err != nil {
		return ConfigValidateResponse{}, err
	}
	if !config.RevisionID(response.Revision).Valid() || response.CanonicalSize <= 0 {
		return ConfigValidateResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: errors.New("validation response is invalid")}
	}
	return response, nil
}

func (c *Client) ApplyConfig(ctx context.Context, tomlBytes []byte, proof ConfigValidateResponse) (ConfigMutationResponse, error) {
	if len(tomlBytes) > config.MaxSourceBytes || !utf8.Valid(tomlBytes) || !config.RevisionID(proof.Revision).Valid() {
		return ConfigMutationResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: errors.New("apply request is invalid")}
	}
	body, err := json.Marshal(ConfigApplyRequest{TOML: string(tomlBytes), ExpectedRevision: proof.Revision, ExpectedGeneration: proof.BaseGeneration})
	if err != nil {
		return ConfigMutationResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: err}
	}
	var response ConfigMutationResponse
	if err := c.request(ctx, http.MethodPost, "/v1/config/apply", body, &response); err != nil {
		return ConfigMutationResponse{}, err
	}
	if err := validateConfigMutation(response); err != nil {
		return ConfigMutationResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: err}
	}
	return response, nil
}

func (c *Client) RollbackConfig(ctx context.Context) (ConfigMutationResponse, error) {
	var response ConfigMutationResponse
	if err := c.request(ctx, http.MethodPost, "/v1/config/rollback", nil, &response); err != nil {
		return ConfigMutationResponse{}, err
	}
	if err := validateConfigMutation(response); err != nil {
		return ConfigMutationResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: err}
	}
	return response, nil
}

func (c *Client) CloseIdleConnections() { c.transport.CloseIdleConnections() }

func (c *Client) request(ctx context.Context, method, path string, requestBody []byte, destination any) error {
	requestContext, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	token, err := c.tokenSource.Token(requestContext)
	if err != nil {
		if kind := contextErrorKind(requestContext); kind != "" {
			return &ClientError{Kind: kind, Err: err}
		}
		return &ClientError{Kind: ClientErrorToken, Err: err}
	}
	var body io.Reader
	if requestBody != nil {
		body = bytes.NewReader(requestBody)
	}
	request, err := http.NewRequestWithContext(requestContext, method, "http://local"+path, body)
	if err != nil {
		return &ClientError{Kind: ClientErrorProtocol, Err: err}
	}
	request.Header.Set("Authorization", "Bearer "+token.String())
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if kind := contextErrorKind(requestContext); kind != "" {
			return &ClientError{Kind: kind, Err: err}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return &ClientError{Kind: ClientErrorTimeout, Err: err}
		}
		if errors.Is(err, context.Canceled) {
			return &ClientError{Kind: ClientErrorCancelled, Err: err}
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return &ClientError{Kind: ClientErrorTimeout, Err: err}
		}
		return &ClientError{Kind: ClientErrorConnection, Err: err}
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		closeErr := response.Body.Close()
		return &ClientError{Kind: ClientErrorProtocol, Status: response.StatusCode, RequestID: response.Header.Get(RequestIDHeader), Err: errors.Join(errors.New("response content type is invalid"), closeErr)}
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBody+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		if kind := contextErrorKind(requestContext); kind != "" {
			return &ClientError{Kind: kind, Status: response.StatusCode, Err: errors.Join(readErr, closeErr)}
		}
		return &ClientError{Kind: ClientErrorProtocol, Status: response.StatusCode, Err: errors.Join(readErr, closeErr)}
	}
	if closeErr != nil {
		return &ClientError{Kind: ClientErrorProtocol, Status: response.StatusCode, Err: closeErr}
	}
	if int64(len(data)) > c.maxResponseBody {
		return &ClientError{Kind: ClientErrorProtocol, Status: response.StatusCode, Err: errors.New("response body is too large")}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope ErrorResponse
		if err := strictDecodeJSON(data, &envelope); err != nil {
			return &ClientError{Kind: ClientErrorProtocol, Status: response.StatusCode, Err: err}
		}
		if !validAPIErrorCode(envelope.Error.Code) {
			return &ClientError{Kind: ClientErrorProtocol, Status: response.StatusCode, Err: errors.New("API error code is invalid")}
		}
		requestID := envelope.Error.RequestID
		if requestID == "" {
			requestID = response.Header.Get(RequestIDHeader)
		}
		return &ClientError{Kind: ClientErrorAPI, Status: response.StatusCode, API: envelope.Error.Code, RequestID: requestID}
	}
	if err := strictDecodeJSON(data, destination); err != nil {
		return &ClientError{Kind: ClientErrorProtocol, Status: response.StatusCode, RequestID: response.Header.Get(RequestIDHeader), Err: err}
	}
	return nil
}

func validateConfigCurrent(response ConfigCurrentResponse) error {
	switch response.State {
	case "awaiting_configuration", "degraded":
		if response.Revision != "" || response.Generation != 0 || response.Status != "" || response.ActivatedAt != nil {
			return errors.New("configuration state response is invalid")
		}
		return nil
	case "running":
		if !config.RevisionID(response.Revision).Valid() || response.Generation == 0 || response.Status == "" || response.ActivatedAt == nil || response.ActivatedAt.IsZero() {
			return errors.New("configuration state response is invalid")
		}
		return nil
	default:
		return errors.New("configuration state response is invalid")
	}
}

func validateConfigMutation(response ConfigMutationResponse) error {
	if response.State != "running" || !config.RevisionID(response.Revision).Valid() || response.Generation == 0 || response.Status == "" || response.ActivatedAt.IsZero() {
		return errors.New("configuration mutation response is invalid")
	}
	return nil
}

func validateStatusResponse(response StatusResponse) error {
	if response.DataPlane != nil && !validDataPlaneSummary(*response.DataPlane) {
		return errors.New("status response is invalid")
	}
	return nil
}

func validateHealthResponse(response HealthResponse) error {
	if response.Status != "ok" || response.APIVersion != APIVersion {
		return errors.New("health response is invalid")
	}
	return nil
}

func contextErrorKind(ctx context.Context) ClientErrorKind {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ClientErrorTimeout
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return ClientErrorCancelled
	}
	return ""
}

func validAPIErrorCode(code ErrorCode) bool {
	switch code {
	case CodeUnauthorized, CodeInvalidRequest, CodeNotFound, CodeMethodNotAllowed, CodeBodyTooLarge, CodeUnsupportedMediaType, CodeRequestTimeout, CodeServiceUnavailable, CodeInternalError,
		CodeConfigurationInvalid, CodeConfigurationTooLarge, CodeRevisionMismatch, CodeGenerationConflict, CodeNoConfiguration, CodeNoPreviousRevision, CodeConfigurationUnavailable:
		return true
	default:
		return false
	}
}

func strictDecodeJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("response contains trailing JSON")
	}
	return nil
}
