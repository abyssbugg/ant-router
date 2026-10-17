package configservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/config"
)

func applyTimeout(t *testing.T, service *Service, timeout time.Duration) CurrentResult {
	t.Helper()
	data := canonicalConfig(t, timeout)
	validation, err := service.Validate(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Apply(context.Background(), ApplyRequest{TOML: data, ExpectedRevision: validation.Revision, ExpectedGeneration: validation.BaseGeneration})
	if err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}
	return result
}

func TestActivationCallbackFailureIsReportedAsDiagnostic(t *testing.T) {
	service, _ := newService(t, t.TempDir())
	if _, err := service.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	failing := true
	service.SetActivationCallback(func(config.Snapshot) error {
		if failing {
			return errors.New("provider runtime rebuild failed")
		}
		return nil
	})

	applied := applyTimeout(t, service, 30*time.Second)
	if applied.State != StateRunning || applied.Generation != 1 {
		t.Fatalf("Apply() = %#v; activation itself succeeded and must be reported", applied)
	}
	if !strings.Contains(applied.Diagnostic, "provider runtime rebuild failed") {
		t.Fatalf("Apply() diagnostic = %q, want rebuild failure", applied.Diagnostic)
	}
	if current := service.Current(); !strings.Contains(current.Diagnostic, "provider runtime rebuild failed") {
		t.Fatalf("Current() diagnostic = %q, want rebuild failure", current.Diagnostic)
	}

	failing = false
	if cleared := applyTimeout(t, service, 31*time.Second); cleared.Diagnostic != "" {
		t.Fatalf("Apply() diagnostic after successful rebuild = %q", cleared.Diagnostic)
	}
	if current := service.Current(); current.Diagnostic != "" {
		t.Fatalf("Current() diagnostic after successful rebuild = %q", current.Diagnostic)
	}

	failing = true
	rolled, err := service.Rollback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rolled.Diagnostic, "provider runtime rebuild failed") || !strings.Contains(service.Current().Diagnostic, "provider runtime rebuild failed") {
		t.Fatalf("Rollback() diagnostic = %q, Current() = %q", rolled.Diagnostic, service.Current().Diagnostic)
	}
}
