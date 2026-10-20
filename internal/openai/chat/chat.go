// Package chat maps the canonical protocol to the OpenAI Chat Completions
// dialect spoken by compatible endpoints (DeepSeek, Kimi, GLM, Grok, Qwen,
// OpenRouter, MiniMax, Ollama, LM Studio, and others). Provider-neutral
// canonical types never appear on the wire; wire shapes are private.
package chat

import (
	"errors"
	"fmt"
)

// Error is a stable, redacted translation failure. It never carries wire
// content.
type Error struct {
	Code    string
	Path    string
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s at %s: %s", e.Code, e.Path, e.Message)
}

func unsupported(path string) error {
	return &Error{Code: "unsupported", Path: path, Message: "input is unavailable on the chat wire"}
}

func invalid(path, message string) error {
	return &Error{Code: "invalid", Path: path, Message: message}
}

// configurationErr reports operator configuration (for example the route's
// upstream model) the chat wire cannot use. It is not client-caused, so it is
// neither invalid nor unsupported and maps to an internal failure.
func configurationErr(path, message string) error {
	return &Error{Code: "configuration", Path: path, Message: message}
}

func protocolErr(path string) error {
	return &Error{Code: "protocol", Path: path, Message: "upstream chat response is invalid"}
}

// IsUnsupported reports whether err is a chat-unsupported canonical input.
func IsUnsupported(err error) bool {
	var target *Error
	if errors.As(err, &target) {
		return target.Code == "unsupported"
	}
	return false
}

// IsInvalid reports whether err is a client-caused invalid input error.
// invalid(...) errors describe the client's request and map to
// CodeInvalidRequest at the provider layer; configuration problems use the
// distinct "configuration" code and are not matched.
func IsInvalid(err error) bool {
	var target *Error
	if errors.As(err, &target) {
		return target.Code == "invalid"
	}
	return false
}
