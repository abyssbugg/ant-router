package main

import (
	"bytes"
	"testing"
)

func TestWriteJSONOutput(t *testing.T) {
	var output bytes.Buffer
	if err := writeJSONOutput(&output, map[string]any{"schema_version": 1, "ok": true}); err != nil {
		t.Fatalf("writeJSONOutput() failed: %v", err)
	}
	if output.String() != "{\"ok\":true,\"schema_version\":1}\n" && output.String() != "{\"schema_version\":1,\"ok\":true}\n" {
		t.Errorf("output = %q", output.String())
	}
}
