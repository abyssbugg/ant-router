package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

const sourceWorkflow = "77777777-7777-4777-8777-777777777777"

type reuseAPI struct {
	*fakeAPI
	source      map[string]any
	sourceJobs  []map[string]any
	sourceCalls []string
	sourceErr   bool
	sourcePage  string
	deadlines   []time.Time
}

func reuseFixture() *reuseAPI {
	f := fixture()
	f.workflow["status"] = "success"
	for _, j := range f.jobs {
		j["status"] = "success"
		if j["type"] == "build" {
			j["started_at"] = epoch.Add(-time.Minute).Format(time.RFC3339Nano)
			j["stopped_at"] = epoch.Format(time.RFC3339Nano)
		}
	}
	r := &reuseAPI{fakeAPI: f, source: map[string]any{
		"id": sourceWorkflow, "pipeline_id": sample.Pipeline,
		"project_slug": sample.Project, "name": "pr-coverage", "status": "canceled",
	}}
	b, _ := json.Marshal(f.jobs)
	_ = json.Unmarshal(b, &r.sourceJobs)
	// The historical Windows jobs did not run; Linux really did succeed.
	r.sourceJobs[2]["status"] = "canceled"
	r.sourceJobs[3]["status"] = "not_run"
	delete(r.sourceJobs[3], "started_at")
	delete(r.sourceJobs[3], "stopped_at")
	delete(f.jobs[1], "started_at")
	delete(f.jobs[1], "stopped_at")
	return r
}

func (r *reuseAPI) Call(ctx context.Context, method, path string) (json.RawMessage, error) {
	if strings.Contains(path, sourceWorkflow) {
		r.sourceCalls = append(r.sourceCalls, method+" "+path)
		deadline, ok := ctx.Deadline()
		if !ok {
			return nil, errors.New("source read without deadline")
		}
		r.deadlines = append(r.deadlines, deadline)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.sourceErr {
			return nil, errors.New("source unavailable")
		}
		if method != "GET" {
			return nil, errors.New("source mutation forbidden")
		}
		if strings.HasSuffix(path, "/job") {
			return json.Marshal(map[string]any{"items": r.sourceJobs, "next_page_token": r.sourcePage})
		}
		return json.Marshal(r.source)
	}
	return r.fakeAPI.Call(ctx, method, path)
}

func reuseArgs(armed bool) []string {
	args := []string{"--project", sample.Project, "--pipeline", sample.Pipeline,
		"--workflow", sample.Workflow, "--branch", sample.Branch, "--revision", sample.Revision,
		"--source-workflow", sourceWorkflow}
	if armed {
		args = append(args, "--allow-cancel")
	}
	return args
}

func TestReusedLinuxTimingRequiresCorroboratedSource(t *testing.T) {
	for _, armed := range []bool{false, true} {
		r := reuseFixture()
		if err := run(context.Background(), reuseArgs(armed), r, &fakeClock{epoch}, io.Discard); err != nil {
			t.Fatalf("armed=%t: %v", armed, err)
		}
		if len(r.sourceCalls) != 2 || r.cancelCount != 0 {
			t.Fatalf("source calls=%v cancellations=%d", r.sourceCalls, r.cancelCount)
		}
		if !r.deadlines[0].Equal(r.deadlines[1]) {
			t.Fatal("source request budget renewed")
		}
	}
}

func TestReusedLinuxRejectsUnverifiedEvidence(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		mutate func(*reuseAPI)
	}{
		{"source identity", "workflow identity mismatch", func(r *reuseAPI) { r.source["id"] = sample.Workflow }},
		{"source pipeline", "workflow identity mismatch", func(r *reuseAPI) { r.source["pipeline_id"] = sourceWorkflow }},
		{"source project", "workflow identity mismatch", func(r *reuseAPI) { r.source["project_slug"] = "gh/other/project" }},
		{"source name", "workflow identity mismatch", func(r *reuseAPI) { r.source["name"] = "other" }},
		{"source active", "source workflow is not terminal", func(r *reuseAPI) { r.source["status"] = "running" }},
		{"source transport", "CircleCI read failed", func(r *reuseAPI) { r.sourceErr = true }},
		{"source pagination", "source job graph incomplete or not stopped", func(r *reuseAPI) { r.sourcePage = "next" }},
		{"source duplicate", "source job graph incomplete or not stopped", func(r *reuseAPI) {
			// Keep every required job; add a distinct map with duplicated identity.
			encoded, err := json.Marshal(r.sourceJobs[1])
			if err != nil {
				t.Fatal(err)
			}
			var duplicate map[string]any
			if err := json.Unmarshal(encoded, &duplicate); err != nil {
				t.Fatal(err)
			}
			r.sourceJobs = append(r.sourceJobs, duplicate)
		}},
		{"source running jobs", "source job graph incomplete or not stopped", func(r *reuseAPI) { r.sourceJobs[3]["status"] = "running" }},
		{"source Linux failure", "source Linux prefix lacks successful approval dependency", func(r *reuseAPI) { r.sourceJobs[1]["status"] = "failed" }},
		{"source approval failure", "source Linux prefix lacks successful approval dependency", func(r *reuseAPI) { r.sourceJobs[0]["status"] = "canceled" }},
		{"source wrong job", "reused Linux identity/status/dependency mismatch", func(r *reuseAPI) { r.sourceJobs[1]["id"] = sourceWorkflow }},
		{"source wrong type", "source Linux prefix lacks successful approval dependency", func(r *reuseAPI) { r.sourceJobs[1]["type"] = "approval" }},
		{"source dependency", "source Linux prefix lacks successful approval dependency", func(r *reuseAPI) { r.sourceJobs[1]["dependencies"] = []string{} }},
		{"source widened result", "source dependency results widened", func(r *reuseAPI) {
			r.sourceJobs[1]["requires"] = map[string][]string{r.jobs[0]["id"].(string): {"failed"}}
		}},
		{"source missing start", "source Linux timing evidence missing", func(r *reuseAPI) { delete(r.sourceJobs[1], "started_at") }},
		{"source missing stop", "source Linux timing evidence missing", func(r *reuseAPI) { delete(r.sourceJobs[1], "stopped_at") }},
		{"source malformed time", "invalid/future job start timestamp", func(r *reuseAPI) { r.sourceJobs[1]["started_at"] = "invalid" }},
		{"source exact deadline", "invalid or over-limit completed job duration", func(r *reuseAPI) { r.sourceJobs[1]["started_at"] = epoch.Add(-jobLimit).Format(time.RFC3339Nano) }},
		{"source stop before start", "invalid or over-limit completed job duration", func(r *reuseAPI) { r.sourceJobs[1]["stopped_at"] = epoch.Add(-time.Hour).Format(time.RFC3339Nano) }},
		{"source future stop", "invalid or over-limit completed job duration", func(r *reuseAPI) { r.sourceJobs[1]["stopped_at"] = epoch.Add(time.Minute).Format(time.RFC3339Nano) }},
		{"current conflicting time", "reused Linux timestamp contradicts source", func(r *reuseAPI) { r.jobs[1]["started_at"] = epoch.Add(-2 * time.Minute).Format(time.RFC3339Nano) }},
		{"current Linux active", "reused Linux identity/status/dependency mismatch", func(r *reuseAPI) { r.jobs[1]["status"] = "running" }},
		{"current Windows missing time", "started job lacks timestamp", func(r *reuseAPI) { delete(r.jobs[3], "started_at") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := reuseFixture()
			tc.mutate(r)
			err := run(context.Background(), reuseArgs(false), r, &fakeClock{epoch}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want rejection reason %q", err, tc.want)
			}
			if r.cancelCount != 0 {
				t.Fatal("read-only check canceled workflow")
			}
		})
	}
}
