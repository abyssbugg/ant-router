package controltoken

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
)

const (
	TokenSize          = 32
	EncodedTokenLength = 43
)

var tokenEncoding = base64.RawURLEncoding.Strict()

// Token is a 256-bit local control credential.
type Token [TokenSize]byte

// Generate reads exactly 32 bytes from random and returns a token.
func Generate(random io.Reader) (Token, error) {
	if random == nil {
		random = rand.Reader
	}
	var token Token
	if _, err := io.ReadFull(random, token[:]); err != nil {
		return Token{}, &Error{Code: CodeGenerationFailed, Err: errors.New("token generation failed")}
	}
	return token, nil
}

// Parse decodes a canonical unpadded base64url token.
func Parse(value string) (Token, error) {
	if len(value) != EncodedTokenLength {
		return Token{}, invalidTokenError()
	}
	decoded, err := tokenEncoding.DecodeString(value)
	if err != nil || len(decoded) != TokenSize {
		return Token{}, invalidTokenError()
	}
	var token Token
	copy(token[:], decoded)
	if token.String() != value {
		return Token{}, invalidTokenError()
	}
	return token, nil
}

// String returns the canonical unpadded base64url representation.
func (t Token) String() string {
	return tokenEncoding.EncodeToString(t[:])
}

// Verify compares a candidate token in constant time after strict parsing.
func (t Token) Verify(candidate string) bool {
	parsed, err := Parse(candidate)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(t[:], parsed[:]) == 1
}

func invalidTokenError() error {
	return &Error{Code: CodeInvalidToken, Path: "token", Err: errors.New("token is invalid")}
}
