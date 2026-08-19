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
	if err := c.get(ctx, "/v1/health", &response); err != nil {
		return HealthResponse{}, err
	}
	if err := validateHealthResponse(response); err != nil {
		return HealthResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: err}
	}
	return response, nil
}

func (c *Client) Version(ctx context.Context) (VersionResponse, error) {
	var response VersionResponse
	if err := c.get(ctx, "/v1/version", &response); err != nil {
		return VersionResponse{}, err
	}
	if response.APIVersion != APIVersion || response.Name != "ant-router" || response.Version == "" {
		return VersionResponse{}, &ClientError{Kind: ClientErrorProtocol, Err: errors.New("version response is invalid")}
	}
	return response, nil
}

func (c *Client) Status(ctx context.Context) (StatusResponse, error) {
	var response StatusResponse
	err := c.get(ctx, "/v1/status", &response)
	return response, err
}

func (c *Client) CloseIdleConnections() { c.transport.CloseIdleConnections() }

func (c *Client) get(ctx context.Context, path string, destination any) error {
	requestContext, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	token, err := c.tokenSource.Token(requestContext)
	if err != nil {
		if kind := contextErrorKind(requestContext); kind != "" {
			return &ClientError{Kind: kind, Err: err}
		}
		return &ClientError{Kind: ClientErrorToken, Err: err}
	}
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, "http://local"+path, nil)
	if err != nil {
		return &ClientError{Kind: ClientErrorProtocol, Err: err}
	}
	request.Header.Set("Authorization", "Bearer "+token.String())
	request.Header.Set("Accept", "application/json")
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
	case CodeUnauthorized, CodeInvalidRequest, CodeNotFound, CodeMethodNotAllowed, CodeBodyTooLarge, CodeUnsupportedMediaType, CodeRequestTimeout, CodeServiceUnavailable, CodeInternalError:
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
