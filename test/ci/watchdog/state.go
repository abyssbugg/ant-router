package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var projectPattern = regexp.MustCompile(`^(circleci|gh|github)/[A-Za-z0-9_-]+/[A-Za-z0-9_.-]+$`)

func (t target) validate() error {
	if !uuidPattern.MatchString(t.Pipeline) || !uuidPattern.MatchString(t.Workflow) || !shaPattern.MatchString(t.Revision) || !projectPattern.MatchString(t.Project) {
		return errors.New("explicit pipeline/workflow UUIDs, canonical project slug and full lowercase SHA required")
	}
	if t.SourceWorkflow != "" && (!uuidPattern.MatchString(t.SourceWorkflow) || t.SourceWorkflow == t.Workflow) {
		return errors.New("source workflow must be a distinct explicit UUID")
	}
	if t.Branch == "" || len(t.Branch) > 255 || strings.ContainsAny(t.Branch, "\x00\r\n\t ") {
		return errors.New("explicit branch required")
	}
	return nil
}

type revisionInfo struct {
	Commit   string `json:"commit_sha"`
	Checkout string `json:"checkout_sha"`
	Branch   string `json:"branch"`
}

type pipelineInfo struct {
	ID      string            `json:"id"`
	Project string            `json:"project_slug"`
	State   string            `json:"state"`
	Errors  []json.RawMessage `json:"errors"`
	Trigger struct {
		Config   revisionInfo `json:"github_app"`
		Checkout revisionInfo `json:"git"`
	} `json:"trigger_parameters"`
}

func (p pipelineInfo) validate(t target) error {
	if p.ID != t.Pipeline || p.Project != t.Project || p.Trigger.Config.Commit != t.Revision || p.Trigger.Checkout.Checkout != t.Revision || p.Trigger.Config.Branch != t.Branch || p.Trigger.Checkout.Branch != t.Branch {
		return errors.New("pipeline identity/config/checkout revision mismatch; no cancellation authorized")
	}
	if p.State != "created" || len(p.Errors) != 0 {
		return errors.New("pipeline is not a valid created pipeline")
	}
	return nil
}

type workflowInfo struct {
	ID       string `json:"id"`
	Pipeline string `json:"pipeline_id"`
	Project  string `json:"project_slug"`
	Name     string `json:"name"`
	Status   string `json:"status"`
}

var (
	errWorkflowIdentity   = errors.New("workflow identity mismatch; no cancellation authorized")
	errIncompleteIdentity = errors.New("workflow response lacks identity")
)

func (w workflowInfo) validate(t target) error {
	// A conflicting nonempty field always wins over missing fields.
	for _, pair := range [][2]string{{w.ID, t.Workflow}, {w.Pipeline, t.Pipeline}, {w.Project, t.Project}, {w.Name, "pr-coverage"}} {
		if pair[0] != "" && pair[0] != pair[1] {
			return errWorkflowIdentity
		}
	}
	if w.ID == "" || w.Pipeline == "" || w.Project == "" || w.Name == "" {
		return errIncompleteIdentity
	}
	return nil
}

func terminal(status string) bool {
	switch status {
	case "success", "failed", "error", "canceled", "unauthorized":
		return true
	}
	return false
}

type jobInfo struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Project      string              `json:"project_slug"`
	Type         string              `json:"type"`
	Status       string              `json:"status"`
	Started      string              `json:"started_at"`
	Stopped      string              `json:"stopped_at"`
	Dependencies []string            `json:"dependencies"`
	Requires     map[string][]string `json:"requires"`
}

type jobPage struct {
	Items []jobInfo `json:"items"`
	Next  string    `json:"next_page_token"`
}

type observedStart struct {
	remote, local time.Time
	age           time.Duration
}
type jobTracker struct {
	starts map[string]observedStart
	ids    map[string]string
}

var jobNames = []string{"approve-linux-coverage", "pr-coverage-linux", "approve-windows-coverage", "pr-coverage-windows"}

func (p jobPage) stopped(t target) bool {
	if p.Next != "" || len(p.Items) != len(jobNames) {
		return false
	}
	names, ids := make(map[string]bool), make(map[string]bool)
	for _, j := range p.Items {
		if j.Project != t.Project || !uuidPattern.MatchString(j.ID) || ids[j.ID] || names[j.Name] {
			return false
		}
		ids[j.ID], names[j.Name] = true, true
		switch j.Status {
		case "success", "failed", "error", "canceled", "unauthorized", "timedout", "infrastructure_fail", "not_run":
		default:
			return false
		}
	}
	for _, name := range jobNames {
		if !names[name] {
			return false
		}
	}
	return true
}

// inspect requires the complete expected graph, including pending approvals.
// Unknown schemas, pagination, skips and bad timestamps cannot certify coverage.
func (p jobPage) inspect(t target, now time.Time, seen jobTracker) (bool, error) {
	if p.Next != "" || len(p.Items) != len(jobNames) {
		return false, errors.New("incomplete or paginated job graph")
	}
	byName := make(map[string]jobInfo)
	ids := make(map[string]bool)
	for _, j := range p.Items {
		if !uuidPattern.MatchString(j.ID) || ids[j.ID] || j.Project != t.Project {
			return false, errors.New("invalid job identity")
		}
		if _, exists := byName[j.Name]; exists {
			return false, errors.New("duplicate job name")
		}
		if prior, exists := seen.ids[j.Name]; exists && prior != j.ID {
			return false, errors.New("job identity changed; possible rerun")
		}
		seen.ids[j.Name] = j.ID
		byName[j.Name], ids[j.ID] = j, true
	}
	allPassed := true
	for i, name := range jobNames {
		j, ok := byName[name]
		typ := "build"
		if i%2 == 0 {
			typ = "approval"
		}
		if !ok || j.Type != typ {
			return false, errors.New("unexpected job graph/type")
		}
		if i == 0 {
			if len(j.Dependencies) != 0 {
				return false, errors.New("invalid root approval")
			}
		} else if len(j.Dependencies) != 1 || j.Dependencies[0] != byName[jobNames[i-1]].ID {
			return false, errors.New("approval dependency bypass")
		}
		if j.Requires != nil {
			if len(j.Requires) != len(j.Dependencies) {
				return false, errors.New("unexpected dependency result rules")
			}
			for _, dep := range j.Dependencies {
				outcomes := j.Requires[dep]
				if len(outcomes) != 1 || outcomes[0] != "success" {
					return false, errors.New("widened dependency result rules")
				}
			}
		}
		switch j.Status {
		case "success", "running", "queued", "blocked", "on_hold", "not_running":
		default:
			return false, fmt.Errorf("job %s is failed/skipped/unknown: %s", name, j.Status)
		}
		if j.Status != "success" {
			allPassed = false
		}
		if i > 0 && (j.Status == "running" || j.Status == "success") && byName[jobNames[i-1]].Status != "success" {
			return false, errors.New("job ran before successful dependency")
		}
		if typ != "build" {
			continue
		}
		if j.Started == "" {
			if _, observed := seen.starts[j.ID]; observed {
				return false, errors.New("job lost start timestamp after starting")
			}
			if j.Status == "running" || j.Status == "success" {
				return false, errors.New("started job lacks timestamp")
			}
			continue
		}
		start, err := time.Parse(time.RFC3339Nano, j.Started)
		if err != nil || start.After(now.Add(maxClockSkew)) {
			return false, errors.New("invalid/future job start timestamp")
		}
		first, exists := seen.starts[j.ID]
		if exists && !start.Equal(first.remote) {
			return false, errors.New("job start changed; possible rerun")
		}
		if !exists {
			// A slightly future remote start must never add time to the
			// observed 15-minute budget. Local elapsed time is monotonic.
			age := now.Sub(start)
			if age < 0 {
				age = 0
			}
			first = observedStart{remote: start, local: now, age: age}
			seen.starts[j.ID] = first
		}
		if j.Status == "success" {
			stop, err := time.Parse(time.RFC3339Nano, j.Stopped)
			if err != nil || stop.Before(start) || stop.After(now.Add(maxClockSkew)) || stop.Sub(start) >= jobLimit {
				return false, errors.New("invalid or over-limit completed job duration")
			}
		} else if now.Sub(start) >= jobLimit || first.age+now.Sub(first.local) >= jobLimit {
			return false, fmt.Errorf("job %s reached the 15-minute deadline", name)
		}
	}
	return allPassed, nil
}
