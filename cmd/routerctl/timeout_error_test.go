package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/abyssbugg/ant-router/internal/control"
)

// A daemon that is reachable but slow must not be reported as unreachable:
// the message is what operators and CI logs act on. Each timeout message
// claims only what its evidence supports: a client-side deadline does not
// prove the daemon received the request, while CodeRequestTimeout is the
// daemon itself reporting that it ran out of time.
func TestDescribeCLIErrorDistinguishesTimeoutFromConnection(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		wantCode    string
		wantMessage string
	}{
		{"client timeout", &control.ClientError{Kind: control.ClientErrorTimeout, Err: context.DeadlineExceeded}, "timeout", "control request timed out"},
		{"wrapped client timeout", fmt.Errorf("apply: %w", &control.ClientError{Kind: control.ClientErrorTimeout, Err: errors.New("i/o timeout")}), "timeout", "control request timed out"},
		{"server request timeout", &control.ClientError{Kind: control.ClientErrorAPI, Status: 408, API: control.CodeRequestTimeout}, "timeout", "daemon request timed out"},
		{"wrapped server request timeout", fmt.Errorf("apply: %w", &control.ClientError{Kind: control.ClientErrorAPI, Status: 408, API: control.CodeRequestTimeout}), "timeout", "daemon request timed out"},
		{"deadline exceeded", context.DeadlineExceeded, "timeout", "operation timed out"},
		{"wrapped deadline exceeded", fmt.Errorf("probe: %w", context.DeadlineExceeded), "timeout", "operation timed out"},
		{"connection", &control.ClientError{Kind: control.ClientErrorConnection, Err: errors.New("connection refused")}, "connection", "unable to connect to daemon"},
		{"cancelled", context.Canceled, "connection", "unable to connect to daemon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCode(tc.err); got != ExitConnection {
				t.Fatalf("exitCode = %d, want %d (every timeout shares the connection exit class)", got, ExitConnection)
			}
			detail := describeCLIError(tc.err)
			if detail.Code != tc.wantCode || detail.Message != tc.wantMessage {
				t.Fatalf("describeCLIError = (%q, %q), want (%q, %q)", detail.Code, detail.Message, tc.wantCode, tc.wantMessage)
			}
		})
	}
}

// Other API errors keep their existing classification; only the request
// timeout code moves to the timeout class.
func TestDescribeCLIErrorKeepsOtherAPIErrorsInternal(t *testing.T) {
	err := &control.ClientError{Kind: control.ClientErrorAPI, Status: 500, API: control.CodeInternalError}
	if got := exitCode(err); got != ExitInternal {
		t.Fatalf("exitCode = %d, want %d", got, ExitInternal)
	}
	if detail := describeCLIError(err); detail.Code != "internal" {
		t.Fatalf("describeCLIError code = %q, want internal", detail.Code)
	}
}
