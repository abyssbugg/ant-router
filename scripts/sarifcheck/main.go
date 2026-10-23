// Command sarifcheck verifies that a codacy-cli SARIF report contains a
// successful run of the requested analyzer and prints its finding count.
//
// Usage: sarifcheck TOOL FILE
//
// codacy-cli v2 exits 0 even when an analyzer fails, so the report itself is
// the only reliable signal: it must parse, contain at least one run whose
// driver is TOOL, and no invocation of that tool may report
// "executionSuccessful": false.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type report struct {
	Runs []struct {
		Tool struct {
			Driver struct {
				Name string `json:"name"`
			} `json:"driver"`
		} `json:"tool"`
		Invocations []struct {
			ExecutionSuccessful *bool `json:"executionSuccessful"`
		} `json:"invocations"`
		Results []json.RawMessage `json:"results"`
	} `json:"runs"`
}

// driverMatches reports whether a SARIF driver name denotes tool: the names
// codacy-cli tools report are the tool id in any case, optionally followed by
// a space-separated qualifier ("Lizard", "Opengrep OSS").
func driverMatches(driver, tool string) bool {
	driver = strings.ToLower(driver)
	return driver == tool || strings.HasPrefix(driver, tool+" ")
}

// check returns the number of findings tool reported in the SARIF data, or an
// error when the data is not a successful run of tool.
func check(tool string, data []byte) (int, error) {
	var parsed report
	if err := json.Unmarshal(data, &parsed); err != nil {
		return 0, fmt.Errorf("parse SARIF: %w", err)
	}
	runs, findings := 0, 0
	for _, run := range parsed.Runs {
		if !driverMatches(run.Tool.Driver.Name, tool) {
			continue
		}
		for _, invocation := range run.Invocations {
			if invocation.ExecutionSuccessful != nil && !*invocation.ExecutionSuccessful {
				return 0, fmt.Errorf("%s run reports executionSuccessful: false", tool)
			}
		}
		runs++
		findings += len(run.Results)
	}
	if runs == 0 {
		return 0, errors.New("no SARIF run for " + tool)
	}
	return findings, nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: sarifcheck TOOL FILE")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[2])
	if err == nil {
		var count int
		if count, err = check(os.Args[1], data); err == nil {
			fmt.Printf("%s: %d finding(s)\n", os.Args[1], count)
			return
		}
	}
	fmt.Fprintln(os.Stderr, "sarifcheck:", err)
	os.Exit(1)
}
