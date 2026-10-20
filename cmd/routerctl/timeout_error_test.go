package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/abyssbugg/ant-router/internal/control"
)

// A daemon that is reachable but slow must not be reported as unreachable:
// the message is what operators and CI logs act on.
func TestDescribeCLIErrorDistinguishesTimeoutFromConnection(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		wantCode    string
		wantMessage string
	}{
		{"client timeout", &control.ClientError{Kind: control.ClientErrorTimeout, Err: context.DeadlineExceeded}, "timeout", "daemon did not respond in time"},
		{"wrapped client timeout", fmt.Errorf("apply: %w", &control.ClientError{Kind: control.ClientErrorTimeout, Err: errors.New("i/o timeout")}), "timeout", "daemon did not respond in time"},
		{"deadline exceeded", context.DeadlineExceeded, "timeout", "daemon did not respond in time"},
		{"connection", &control.ClientError{Kind: control.ClientErrorConnection, Err: errors.New("connection refused")}, "connection", "unable to connect to daemon"},
		{"cancelled", context.Canceled, "connection", "unable to connect to daemon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCode(tc.err); got != ExitConnection {
				t.Fatalf("exitCode = %d, want %d (exit codes are a stable contract)", got, ExitConnection)
			}
			detail := describeCLIError(tc.err)
			if detail.Code != tc.wantCode || detail.Message != tc.wantMessage {
				t.Fatalf("describeCLIError = (%q, %q), want (%q, %q)", detail.Code, detail.Message, tc.wantCode, tc.wantMessage)
			}
		})
	}
}
