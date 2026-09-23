package main

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestReusedLinuxPreservesWindowsDeadline(t *testing.T) {
	r := reuseFixture()
	r.workflow["status"] = "running"
	r.jobs[3]["status"] = "running"
	r.jobs[3]["started_at"] = epoch.Format(time.RFC3339Nano)
	delete(r.jobs[3], "stopped_at")
	c := &fakeClock{epoch}
	err := run(context.Background(), reuseArgs(true), r, c, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "15-minute deadline") || r.cancelCount != 1 || c.now.Sub(epoch) != jobLimit {
		t.Fatalf("err=%v cancels=%d elapsed=%v", err, r.cancelCount, c.now.Sub(epoch))
	}
	if len(r.sourceCalls) != 2 {
		t.Fatalf("source evidence reread: %v", r.sourceCalls)
	}
	for _, call := range r.sourceCalls {
		if !strings.HasPrefix(call, "GET ") {
			t.Fatal("source workflow mutated")
		}
	}
}

func TestReusedLinuxCannotChangeAfterVerification(t *testing.T) {
	for _, field := range []string{"id", "status", "started_at", "stopped_at"} {
		t.Run(field, func(t *testing.T) {
			r := reuseFixture()
			r.workflow["status"] = "on_hold"
			r.jobs[2]["status"] = "on_hold"
			r.jobs[3]["status"] = "blocked"
			delete(r.jobs[3], "started_at")
			delete(r.jobs[3], "stopped_at")
			reads := 0
			r.onCall = func(path string) {
				if path == "api/v2/workflow/"+sample.Workflow+"/job" {
					reads++
					if reads == 2 {
						r.jobs[1][field] = "changed"
						if field == "id" {
							r.jobs[1][field] = sourceWorkflow
						}
					}
				}
			}
			c := &fakeClock{epoch}
			err := run(context.Background(), reuseArgs(true), r, c, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "reused Linux") || r.cancelCount != 1 || c.now.Sub(epoch) != pollInterval {
				t.Fatalf("err=%v cancels=%d elapsed=%v", err, r.cancelCount, c.now.Sub(epoch))
			}
		})
	}
}

func TestReusedLinuxSourceValidationNeverAuthorizesCancellation(t *testing.T) {
	for _, mutate := range []func(*reuseAPI){
		func(r *reuseAPI) { r.source["pipeline_id"] = sourceWorkflow },
		func(r *reuseAPI) { r.sourceErr = true },
		func(r *reuseAPI) {
			r.pipeline["trigger_parameters"].(map[string]any)["git"].(map[string]any)["checkout_sha"] = strings.Repeat("b", 40)
		},
	} {
		r := reuseFixture()
		mutate(r)
		err := run(context.Background(), reuseArgs(true), r, &fakeClock{epoch}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "UNSUPERVISED") || r.cancelCount != 0 {
			t.Fatalf("err=%v cancels=%d", err, r.cancelCount)
		}
	}
}

func TestInvalidSourceWorkflowRejectedBeforeAPI(t *testing.T) {
	for _, source := range []string{sample.Workflow, "../cancel", "not-a-uuid"} {
		r := reuseFixture()
		args := reuseArgs(false)
		args[len(args)-1] = source
		err := run(context.Background(), args, r, &fakeClock{epoch}, io.Discard)
		if err == nil || len(r.calls) != 0 || len(r.sourceCalls) != 0 {
			t.Fatalf("source=%q err=%v calls=%v", source, err, r.calls)
		}
	}
}

func TestReusedLinuxCorroborationIsTransactional(t *testing.T) {
	r := reuseFixture()
	target := sample
	target.SourceWorkflow = sourceWorkflow
	e, err := loadReusedLinux(context.Background(), r, target)
	if err != nil {
		t.Fatal(err)
	}
	var page jobPage
	if err := read(context.Background(), r, "api/v2/workflow/"+sample.Workflow+"/job", &page); err != nil {
		t.Fatal(err)
	}
	filled, err := e.corroborate(page)
	if err != nil || filled.Items[1].Started == "" || page.Items[1].Started != "" {
		t.Fatalf("err=%v original mutated or output incomplete", err)
	}
	page.Items[1].Stopped = "contradiction"
	_, err = e.corroborate(page)
	if err == nil || page.Items[1].Started != "" {
		t.Fatal("rejected evidence partially mutated graph")
	}
}

func TestAPILatencyCountsTowardJobDeadline(t *testing.T) {
	f := fixture()
	c := &fakeClock{epoch}
	f.jobs[1]["started_at"] = epoch.Add(-jobLimit + 5*time.Second).Format(time.RFC3339Nano)
	reads := 0
	f.onCall = func(path string) {
		if path == "api/v2/workflow/"+sample.Workflow+"/job" {
			reads++
			if reads == 1 {
				c.now = c.now.Add(5 * time.Second)
			}
		}
	}
	err := monitor(context.Background(), f, c, sample)
	if err == nil || !strings.Contains(err.Error(), "15-minute deadline") || f.cancelCount != 1 || c.now.Sub(epoch) != 5*time.Second {
		t.Fatalf("err=%v cancels=%d elapsed=%v", err, f.cancelCount, c.now.Sub(epoch))
	}
}
