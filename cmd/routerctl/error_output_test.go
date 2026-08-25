package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/controltoken"
)

func TestExitCodeDirectTokenErrors(t *testing.T) {
	tests := []struct {
		code controltoken.Code
		want int
	}{
		{controltoken.CodeTokenNotFound, ExitAuthentication},
		{controltoken.CodeInvalidToken, ExitAuthentication},
		{controltoken.CodeTokenCorrupt, ExitAuthentication},
		{controltoken.CodePermissionDenied, ExitConnection},
		{controltoken.CodeStoreUnavailable, ExitConnection},
		{controltoken.CodeDurabilityUncertain, ExitInternal},
	}
	for _, test := range tests {
		err := &controltoken.Error{Code: test.code, Path: "synthetic/private/path", Err: errors.New("synthetic secret marker")}
		if got := exitCode(err); got != test.want {
			t.Errorf("exitCode(%q) = %d, want %d", test.code, got, test.want)
		}
	}
}

func TestCLIErrorOutputIsStableAndRedacted(t *testing.T) {
	err := &controltoken.Error{Code: controltoken.CodeTokenNotFound, Path: "synthetic/private/path", Err: errors.New("synthetic secret marker")}
	for _, jsonMode := range []bool{false, true} {
		var output bytes.Buffer
		if writeErr := writeCLIError(&output, cliOptions{JSON: jsonMode}, err); writeErr != nil {
			t.Fatalf("writeCLIError() failed: %v", writeErr)
		}
		text := output.String()
		if strings.Contains(text, "synthetic/private/path") || strings.Contains(text, "synthetic secret marker") {
			t.Errorf("error output leaked details: %q", text)
		}
		if jsonMode && (!strings.Contains(text, `"code":"authentication"`) || !strings.Contains(text, `"exit_code":4`)) {
			t.Errorf("JSON error schema = %q", text)
		}
	}
}

func TestMissingConfigurationFileIsSafeValidationError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "private", "missing.toml")
	client := &fakeControlClient{}
	var stderr bytes.Buffer
	code := execute(context.Background(), cliOptions{}, cliCommand{Name: "config", Action: "validate", Input: missing}, bytes.NewReader(nil), &bytes.Buffer{}, &stderr, client)
	if code != ExitValidation {
		t.Fatalf("exit code = %d, want %d", code, ExitValidation)
	}
	if strings.Contains(stderr.String(), missing) {
		t.Errorf("stderr exposed input path: %q", stderr.String())
	}
}
