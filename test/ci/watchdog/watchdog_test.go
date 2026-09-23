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

var sample = target{
	Project: "circleci/example/project", Pipeline: "11111111-1111-4111-8111-111111111111",
	Workflow: "22222222-2222-4222-8222-222222222222", Branch: "test/windows-token-regression",
	Revision: strings.Repeat("a", 40),
}

var epoch = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.now = c.now.Add(d)
	return nil
}

type fakeAPI struct {
	pipeline        map[string]any
	workflow        map[string]any
	jobs            []map[string]any
	calls           []string
	cancelCount     int
	cancelError     bool
	confirm         bool
	keepJobsRunning bool
	brokenJobs      bool
	pageToken       string
	onCall          func(string)
}

func fixture() *fakeAPI {
	return &fakeAPI{
		pipeline: map[string]any{
			"id": sample.Pipeline, "project_slug": sample.Project, "state": "created", "errors": []any{},
			"trigger_parameters": map[string]any{
				"github_app": map[string]any{"commit_sha": sample.Revision, "branch": sample.Branch, "repo_url": "https://github.com/abyssbugg/ant-router"},
				"git":        map[string]any{"checkout_sha": sample.Revision, "branch": sample.Branch, "repo_url": "https://github.com/abyssbugg/ant-router"},
			},
		},
		workflow: map[string]any{"id": sample.Workflow, "pipeline_id": sample.Pipeline, "project_slug": sample.Project, "name": "pr-coverage", "status": "running"},
		jobs: []map[string]any{
			{"id": "33333333-3333-4333-8333-333333333333", "name": "approve-linux-coverage", "type": "approval", "status": "success", "dependencies": []string{}, "project_slug": sample.Project},
			{"id": "44444444-4444-4444-8444-444444444444", "name": "pr-coverage-linux", "type": "build", "status": "running", "started_at": epoch.Add(-jobLimit).Format(time.RFC3339Nano), "dependencies": []string{"33333333-3333-4333-8333-333333333333"}, "project_slug": sample.Project},
			{"id": "55555555-5555-4555-8555-555555555555", "name": "approve-windows-coverage", "type": "approval", "status": "blocked", "dependencies": []string{"44444444-4444-4444-8444-444444444444"}, "project_slug": sample.Project},
			{"id": "66666666-6666-4666-8666-666666666666", "name": "pr-coverage-windows", "type": "build", "status": "blocked", "dependencies": []string{"55555555-5555-4555-8555-555555555555"}, "project_slug": sample.Project},
		}, confirm: true,
	}
}

func (f *fakeAPI) Call(ctx context.Context, method, path string) (json.RawMessage, error) {
	f.calls = append(f.calls, method+" "+path)
	if f.onCall != nil {
		f.onCall(path)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var v any
	switch {
	case method == "POST" && path == "api/v2/workflow/"+sample.Workflow+"/cancel":
		f.cancelCount++
		if f.confirm {
			f.workflow["status"] = "canceled"
			if !f.keepJobsRunning {
				for _, job := range f.jobs {
					if job["status"] != "success" {
						job["status"] = "canceled"
					}
				}
			}
		}
		if f.cancelError {
			return nil, errors.New("transport failure")
		}
		v = map[string]any{"message": "accepted"}
	case method == "GET" && path == "api/v2/pipeline/"+sample.Pipeline:
		v = f.pipeline
	case method == "GET" && path == "api/v2/workflow/"+sample.Workflow:
		v = f.workflow
	case method == "GET" && path == "api/v2/workflow/"+sample.Workflow+"/job":
		if f.brokenJobs {
			return nil, errors.New("transport failure")
		}
		v = map[string]any{"items": f.jobs, "next_page_token": f.pageToken}
	default:
		return nil, errors.New("unexpected API operation: " + method + " " + path)
	}
	return json.Marshal(v)
}

func TestReadOnlyPreflightNeverMutates(t *testing.T) {
	f := fixture()
	args := []string{"--project", sample.Project, "--pipeline", sample.Pipeline, "--workflow", sample.Workflow, "--branch", sample.Branch, "--revision", sample.Revision}
	if err := run(context.Background(), args, f, &fakeClock{epoch}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || f.cancelCount != 0 {
		t.Fatalf("unexpected calls: %v", f.calls)
	}
	if err := run(context.Background(), append(args, "--allow-cancel"), f, &fakeClock{epoch}, io.Discard); err == nil || f.cancelCount != 1 {
		t.Fatalf("err=%v cancels=%d", err, f.cancelCount)
	}
}

func TestTransportAllowlist(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		for _, path := range []string{"api/v2/pipeline/" + sample.Pipeline, "api/v2/workflow/" + sample.Workflow, "api/v2/workflow/" + sample.Workflow + "/job", "api/v2/workflow/" + sample.Workflow + "/cancel", "api/v2/workflow/" + sample.Workflow + "/rerun", "api/v2/workflow/" + sample.Workflow + "/approve/123", "api/v2/pipeline", "https://evil.test/api", "api/v2/workflow/../cancel"} {
			want := method == "GET" && (path == "api/v2/pipeline/"+sample.Pipeline || path == "api/v2/workflow/"+sample.Workflow || path == "api/v2/workflow/"+sample.Workflow+"/job") || method == "POST" && path == "api/v2/workflow/"+sample.Workflow+"/cancel"
			_, err := apiArgs(method, path)
			if (err == nil) != want {
				t.Errorf("%s %s: err=%v", method, path, err)
			}
		}
	}
}

func TestBoundedCLIOutput(t *testing.T) {
	var b boundedBuffer
	if _, err := b.Write(make([]byte, maxResponseBytes)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("x")); err == nil {
		t.Fatal("oversized API output accepted")
	}
	if b.Len() > maxResponseBytes {
		t.Fatal("buffer exceeded bound")
	}
}

func TestSuccessRequiresAllJobsAndNoCancellation(t *testing.T) {
	for _, mode := range []string{"pass", "missing", "skip", "overdue"} {
		t.Run(mode, func(t *testing.T) {
			f := fixture()
			f.workflow["status"] = "success"
			for _, job := range f.jobs {
				job["status"] = "success"
				if job["type"] == "build" {
					job["started_at"] = epoch.Add(-time.Minute).Format(time.RFC3339Nano)
					job["stopped_at"] = epoch.Format(time.RFC3339Nano)
				}
			}
			switch mode {
			case "missing":
				f.jobs = f.jobs[:3]
			case "skip":
				f.jobs[3]["status"] = "not_run"
			case "overdue":
				f.jobs[1]["started_at"] = epoch.Add(-jobLimit).Format(time.RFC3339Nano)
			}
			err := monitor(context.Background(), f, &fakeClock{epoch}, sample)
			if (err == nil) != (mode == "pass") || f.cancelCount != 0 {
				t.Fatalf("err=%v cancels=%d", err, f.cancelCount)
			}
		})
	}
}

func TestInterruptedMonitorConfirmsCancellation(t *testing.T) {
	f := fixture()
	f.jobs[1]["started_at"] = epoch.Format(time.RFC3339Nano)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.onCall = func(path string) {
		if strings.HasSuffix(path, "/job") {
			cancel()
		}
	}
	err := monitor(ctx, f, &fakeClock{epoch}, sample)
	if err == nil || !strings.Contains(err.Error(), "terminal=canceled") || f.cancelCount != 1 {
		t.Fatalf("err=%v cancels=%d", err, f.cancelCount)
	}
}

func TestLostCancellationResponseIsNotRetried(t *testing.T) {
	f := fixture()
	f.cancelError = true
	err := monitor(context.Background(), f, &fakeClock{epoch}, sample)
	if err == nil || !strings.Contains(err.Error(), "terminal=canceled") || f.cancelCount != 1 {
		t.Fatalf("err=%v cancels=%d", err, f.cancelCount)
	}
}

func TestApprovalWaitIsBoundedWithoutStartingJobs(t *testing.T) {
	f := fixture()
	f.workflow["status"] = "on_hold"
	f.jobs[0]["status"] = "on_hold"
	f.jobs[1]["status"] = "blocked"
	delete(f.jobs[1], "started_at")
	c := &fakeClock{epoch}
	err := monitor(context.Background(), f, c, sample)
	if err == nil || f.cancelCount != 1 || c.now.Sub(epoch) != sessionLimit {
		t.Fatalf("err=%v cancels=%d elapsed=%v", err, f.cancelCount, c.now.Sub(epoch))
	}
}

func TestOverdueJobCancelsAndConfirms(t *testing.T) {
	f := fixture()
	err := monitor(context.Background(), f, &fakeClock{epoch}, sample)
	if err == nil || !strings.Contains(err.Error(), "terminal=canceled") || f.cancelCount != 1 {
		t.Fatalf("err=%v cancel count=%d calls=%v", err, f.cancelCount, f.calls)
	}
}

func TestWrongIdentityNeverCancels(t *testing.T) {
	for _, field := range []string{"pipeline", "workflow", "project", "config-sha", "checkout-sha", "branch"} {
		t.Run(field, func(t *testing.T) {
			f := fixture()
			params := f.pipeline["trigger_parameters"].(map[string]any)
			switch field {
			case "pipeline":
				f.pipeline["id"] = sample.Workflow
			case "workflow":
				f.workflow["pipeline_id"] = sample.Workflow
			case "project":
				f.workflow["project_slug"] = "other/project"
			case "config-sha":
				params["github_app"].(map[string]any)["commit_sha"] = strings.Repeat("b", 40)
			case "checkout-sha":
				params["git"].(map[string]any)["checkout_sha"] = strings.Repeat("b", 40)
			case "branch":
				params["git"].(map[string]any)["branch"] = "main"
			}
			err := monitor(context.Background(), f, &fakeClock{epoch}, sample)
			if err == nil || f.cancelCount != 0 {
				t.Fatalf("err=%v cancels=%d", err, f.cancelCount)
			}
		})
	}
}

func TestIncompleteCoverageNeverSucceeds(t *testing.T) {
	cases := []struct {
		name       string
		wantReason string
	}{
		{"empty", "incomplete or paginated job graph"},
		{"skipped", "failed/skipped/unknown"},
		{"failed", "failed/skipped/unknown"},
		{"duplicate", "invalid job identity"},
		{"future", "invalid/future job start timestamp"},
		{"missing-time", "started job lacks timestamp"},
		{"page", "incomplete or paginated job graph"},
		{"transport", "CircleCI read failed"},
		{"approval-bypass", "approval dependency bypass"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture()
			// A deadline failure must not mask the invalid state injected below.
			f.jobs[1]["started_at"] = epoch.Format(time.RFC3339Nano)
			switch tc.name {
			case "empty":
				f.jobs = nil
			case "skipped":
				f.jobs[1]["status"] = "not_run"
			case "failed":
				f.jobs[1]["status"] = "failed"
			case "duplicate":
				f.jobs[3]["id"] = f.jobs[1]["id"]
			case "future":
				f.jobs[1]["started_at"] = epoch.Add(time.Minute).Format(time.RFC3339Nano)
			case "missing-time":
				delete(f.jobs[1], "started_at")
			case "page":
				f.pageToken = "more"
			case "transport":
				f.brokenJobs = true
			case "approval-bypass":
				f.jobs[1]["dependencies"] = []string{}
			}
			err := monitor(context.Background(), f, &fakeClock{epoch}, sample)
			if err == nil || !strings.Contains(err.Error(), tc.wantReason) || f.cancelCount != 1 {
				t.Fatalf("err=%v, want reason %q; cancels=%d, want 1", err, tc.wantReason, f.cancelCount)
			}
		})
	}
}

func TestCancellationIsNotAssumed(t *testing.T) {
	f := fixture()
	f.confirm = false
	c := &fakeClock{epoch}
	err := monitor(context.Background(), f, c, sample)
	if err == nil || !strings.Contains(err.Error(), "UNCONFIRMED") || f.cancelCount != 1 || c.now.Sub(epoch) > confirmationLimit+pollInterval {
		t.Fatalf("err=%v cancels=%d elapsed=%v", err, f.cancelCount, c.now.Sub(epoch))
	}
}
