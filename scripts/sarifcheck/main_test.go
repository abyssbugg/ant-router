package main

import (
	"strings"
	"testing"
)

func TestCheckCountsResultsOfTheMatchingRun(t *testing.T) {
	sarif := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Opengrep OSS","rules":[{"name":"opengrep"}]}},"invocations":[{"executionSuccessful":true}],"results":[{"ruleId":"a"},{"ruleId":"b"}]}]}`
	count, err := check("opengrep", []byte(sarif))
	if err != nil || count != 2 {
		t.Fatalf("check() = %d, %v; want 2, nil", count, err)
	}
}

func TestCheckRejectsUnusableSARIF(t *testing.T) {
	for name, sarif := range map[string]string{
		"empty":            ``,
		"not json":         `{"runs":`,
		"no runs":          `{"version":"2.1.0","runs":[]}`,
		"other tool":       `{"runs":[{"tool":{"driver":{"name":"Trivy"}},"results":[]}]}`,
		"rule name only":   `{"runs":[{"tool":{"driver":{"name":"Trivy","rules":[{"name":"lizard"}]}},"results":[]}]}`,
		"failed execution": `{"runs":[{"tool":{"driver":{"name":"Lizard"}},"invocations":[{"executionSuccessful":false}],"results":[]}]}`,
		"failed other run": `{"runs":[{"tool":{"driver":{"name":"Lizard"}},"results":[]},{"tool":{"driver":{"name":"Lizard"}},"invocations":[{"executionSuccessful":false}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := check("lizard", []byte(sarif)); err == nil {
				t.Fatal("check() accepted unusable SARIF")
			}
		})
	}
}

func TestCheckMatchesDriverNameCaseInsensitively(t *testing.T) {
	for tool, driver := range map[string]string{"lizard": "Lizard", "revive": "revive", "trivy": "Trivy", "opengrep": "Opengrep OSS"} {
		sarif := `{"runs":[{"tool":{"driver":{"name":"` + driver + `"}},"results":[]}]}`
		if _, err := check(tool, []byte(sarif)); err != nil {
			t.Fatalf("check(%q) with driver %q: %v", tool, driver, err)
		}
	}
	if _, err := check("revive", []byte(`{"runs":[{"tool":{"driver":{"name":"reviver"}},"results":[]}]}`)); err == nil || !strings.Contains(err.Error(), "revive") {
		t.Fatalf("check() accepted a driver that only shares a prefix: %v", err)
	}
}
