package buildinfo

import (
	"testing"
)

func TestCurrent(t *testing.T) {
	info := Current()

	if info.Version != "dev" {
		t.Errorf("expected version to be 'dev', got '%s'", info.Version)
	}

	if info.Commit != "unknown" {
		t.Errorf("expected commit to be 'unknown', got '%s'", info.Commit)
	}

	if info.Date != "unknown" {
		t.Errorf("expected date to be 'unknown', got '%s'", info.Date)
	}
}
