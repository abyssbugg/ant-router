package control

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
)

const (
	RequestIDHeader    = "X-Request-ID"
	RequestIDBytes     = 16
	MaxRequestIDLength = 64
)

type requestIDContextKey struct{}

func GenerateRequestID(random io.Reader) (string, error) {
	if random == nil {
		random = rand.Reader
	}
	material := make([]byte, RequestIDBytes)
	if _, err := io.ReadFull(random, material); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(material), nil
}

func ValidRequestID(value string) bool {
	if len(value) < 1 || len(value) > MaxRequestIDLength {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.') {
			return false
		}
	}
	return true
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	if !ValidRequestID(requestID) {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}
