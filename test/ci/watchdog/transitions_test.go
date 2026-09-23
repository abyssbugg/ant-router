package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPollingReachesJobDeadline(t *testing.T) {
	f := fixture()
	f.jobs[1]["started_at"] = epoch.Add(-jobLimit + pollInterval).Format(time.RFC3339Nano)
	c := &fakeClock{epoch}
	err := monitor(context.Background(), f, c, sample)
	if err == nil || f.cancelCount != 1 || c.now.Sub(epoch) != pollInterval {
		t.Fatalf("err=%v cancels=%d elapsed=%v", err, f.cancelCount, c.now.Sub(epoch))
	}
}

func TestPendingNotRunningIsNotFailure(t *testing.T) {
	f := fixture()
	f.workflow["status"] = "on_hold"
	f.jobs[0]["status"] = "on_hold"
	f.jobs[1]["status"] = "not_running"
	delete(f.jobs[1], "started_at")
	c := &fakeClock{epoch}
	err := monitor(context.Background(), f, c, sample)
	if err == nil || !strings.Contains(err.Error(), "session deadline") || c.now.Sub(epoch) != sessionLimit {
		t.Fatalf("err=%v elapsed=%v", err, c.now.Sub(epoch))
	}
}

func TestDependencyResultWideningIsRejected(t *testing.T) {
	f := fixture()
	f.jobs[1]["started_at"] = epoch.Format(time.RFC3339Nano)
	f.jobs[1]["requires"] = map[string][]string{"33333333-3333-4333-8333-333333333333": {"success", "failed"}}
	c := &fakeClock{epoch}
	err := monitor(context.Background(), f, c, sample)
	if err == nil || !strings.Contains(err.Error(), "dependency result") || c.now.Sub(epoch) != 0 || f.cancelCount != 1 {
		t.Fatalf("err=%v elapsed=%v cancels=%d", err, c.now.Sub(epoch), f.cancelCount)
	}
}

func TestJobIdentityCannotChangeDuringMonitoring(t *testing.T) {
	f := fixture()
	f.jobs[1]["started_at"] = epoch.Format(time.RFC3339Nano)
	reads := 0
	f.onCall = func(path string) {
		if strings.HasSuffix(path, "/job") {
			reads++
			if reads == 2 {
				f.jobs[1]["id"] = "77777777-7777-4777-8777-777777777777"
				f.jobs[2]["dependencies"] = []string{"77777777-7777-4777-8777-777777777777"}
			}
		}
	}
	c := &fakeClock{epoch}
	err := monitor(context.Background(), f, c, sample)
	if err == nil || !strings.Contains(err.Error(), "job identity changed") || c.now.Sub(epoch) != pollInterval {
		t.Fatalf("err=%v elapsed=%v", err, c.now.Sub(epoch))
	}
}

func TestTerminalWorkflowWithRunningJobIsUnconfirmed(t *testing.T) {
	f := fixture()
	f.keepJobsRunning = true
	err := monitor(context.Background(), f, &fakeClock{epoch}, sample)
	if err == nil || !strings.Contains(err.Error(), "UNCONFIRMED") || f.cancelCount != 1 {
		t.Fatalf("err=%v cancels=%d", err, f.cancelCount)
	}
}

func TestStartedJobCannotLoseTimestamp(t *testing.T) {
	f := fixture()
	f.jobs[1]["started_at"] = epoch.Format(time.RFC3339Nano)
	reads := 0
	f.onCall = func(path string) {
		if strings.HasSuffix(path, "/job") {
			reads++
			if reads == 2 {
				delete(f.jobs[1], "started_at")
				f.jobs[1]["status"] = "blocked"
			}
		}
	}
	c := &fakeClock{epoch}
	err := monitor(context.Background(), f, c, sample)
	if err == nil || !strings.Contains(err.Error(), "lost start timestamp") || c.now.Sub(epoch) != pollInterval {
		t.Fatalf("err=%v elapsed=%v", err, c.now.Sub(epoch))
	}
}

func TestWorkflowPersistentMissingIdentityCannotReusePriorValues(t *testing.T) {
	f := fixture()
	f.jobs[1]["started_at"] = epoch.Format(time.RFC3339Nano)
	reads := 0
	f.onCall = func(path string) {
		if path == "api/v2/workflow/"+sample.Workflow {
			reads++
			if reads == 2 {
				delete(f.workflow, "id")
			}
		}
	}
	c := &fakeClock{epoch}
	err := monitor(context.Background(), f, c, sample)
	if err == nil || !strings.Contains(err.Error(), "UNCONFIRMED") || !strings.Contains(err.Error(), "lacks identity") || f.cancelCount != 1 || c.now.Sub(epoch) != pollInterval+confirmationLimit {
		t.Fatalf("err=%v cancels=%d elapsed=%v", err, f.cancelCount, c.now.Sub(epoch))
	}
}

func TestInvalidArgumentsMakeNoAPICalls(t *testing.T) {
	for _, args := range [][]string{nil, {"--allow-cancel"}, {"--project", "x/../../cancel"}, {"--unknown"}} {
		f := fixture()
		if err := run(context.Background(), args, f, &fakeClock{epoch}, &strings.Builder{}); err == nil || len(f.calls) != 0 {
			t.Fatalf("err=%v calls=%v", err, f.calls)
		}
	}
}
