package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type identityAPI func(context.Context, string, string) (json.RawMessage, error)

func (f identityAPI) Call(ctx context.Context, method, path string) (json.RawMessage, error) {
	return f(ctx, method, path)
}

func TestIdentityRereadsShareOneBudget(t *testing.T) {
	for _, parentLimit := range []time.Duration{0, time.Second} {
		t.Run(parentLimit.String(), func(t *testing.T) {
			ctx := context.Background()
			if parentLimit > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, parentLimit)
				defer cancel()
			}
			before := time.Now()
			var firstDeadline time.Time
			calls := 0
			client := identityAPI(func(ctx context.Context, method, path string) (json.RawMessage, error) {
				if method != "GET" || path != "api/v2/workflow/"+sample.Workflow {
					t.Fatalf("unexpected operation: %s %s", method, path)
				}
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("read has no timeout")
				}
				calls++
				if calls == 1 {
					firstDeadline = deadline
				} else if !deadline.Equal(firstDeadline) {
					t.Fatalf("deadline renewed: %v -> %v", firstDeadline, deadline)
				}
				return json.RawMessage(`{}`), nil
			})
			_, err := readBoundWorkflow(ctx, client, sample)
			if !errors.Is(err, errIncompleteIdentity) || calls != 3 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			if firstDeadline.Before(before) || firstDeadline.After(time.Now().Add(10*time.Second)) {
				t.Fatalf("unexpected budget: %v", firstDeadline)
			}
			if parentDeadline, ok := ctx.Deadline(); ok && !firstDeadline.Equal(parentDeadline) {
				t.Fatalf("parent deadline not preserved: %v != %v", firstDeadline, parentDeadline)
			}
		})
	}
}

func TestTransientIncompleteIdentityRecovers(t *testing.T) {
	for _, field := range []string{"id", "pipeline_id", "project_slug", "name"} {
		t.Run(field, func(t *testing.T) {
			f := fixture()
			f.jobs[1]["started_at"] = epoch.Add(-jobLimit + 2*pollInterval).Format(time.RFC3339Nano)
			value := f.workflow[field]
			reads := 0
			f.onCall = func(path string) {
				if path == "api/v2/workflow/"+sample.Workflow {
					reads++
					if reads == 2 {
						delete(f.workflow, field)
					} else {
						f.workflow[field] = value
					}
				}
			}
			c := &fakeClock{epoch}
			err := monitor(context.Background(), f, c, sample)
			if err == nil || !strings.Contains(err.Error(), "15-minute deadline") || f.cancelCount != 1 || c.now.Sub(epoch) != 2*pollInterval {
				t.Fatalf("err=%v cancels=%d elapsed=%v", err, f.cancelCount, c.now.Sub(epoch))
			}
		})
	}
}

func TestPersistentIncompleteIdentityCancelsBoundTarget(t *testing.T) {
	for _, field := range []string{"id", "pipeline_id", "project_slug", "name"} {
		t.Run(field, func(t *testing.T) {
			f := fixture()
			f.jobs[1]["started_at"] = epoch.Format(time.RFC3339Nano)
			value := f.workflow[field]
			reads, readsAtCancel := 0, 0
			f.onCall = func(path string) {
				if path == "api/v2/workflow/"+sample.Workflow {
					reads++
					if reads >= 2 && f.cancelCount == 0 {
						delete(f.workflow, field)
					} else {
						f.workflow[field] = value
					}
				}
				if strings.HasSuffix(path, "/cancel") {
					readsAtCancel = reads
				}
			}
			c := &fakeClock{epoch}
			err := monitor(context.Background(), f, c, sample)
			if err == nil || !strings.Contains(err.Error(), "lacks identity") || !strings.Contains(err.Error(), "jobs stopped") || f.cancelCount != 1 || readsAtCancel != 4 || c.now.Sub(epoch) != pollInterval {
				t.Fatalf("err=%v cancels=%d readsAtCancel=%d elapsed=%v", err, f.cancelCount, readsAtCancel, c.now.Sub(epoch))
			}
		})
	}
}

func TestContradictoryIdentityNeverCancelsAndWarns(t *testing.T) {
	fields := []string{"id", "pipeline_id", "project_slug", "name"}
	for i, field := range fields {
		t.Run(field, func(t *testing.T) {
			f := fixture()
			f.jobs[1]["started_at"] = epoch.Format(time.RFC3339Nano)
			reads := 0
			f.onCall = func(path string) {
				if path == "api/v2/workflow/"+sample.Workflow {
					reads++
					if reads == 2 {
						f.workflow[field] = "contradictory"
						delete(f.workflow, fields[(i+1)%len(fields)])
					}
				}
			}
			err := monitor(context.Background(), f, &fakeClock{epoch}, sample)
			if err == nil || !strings.Contains(err.Error(), "UNSUPERVISED") || !strings.Contains(err.Error(), "may still be running") || !strings.Contains(err.Error(), "manually") || f.cancelCount != 0 || reads != 2 {
				t.Fatalf("err=%v cancels=%d reads=%d", err, f.cancelCount, reads)
			}
		})
	}
}

func TestInitialIncompleteIdentityNeverCancels(t *testing.T) {
	for _, field := range []string{"id", "pipeline_id", "project_slug", "name"} {
		t.Run(field, func(t *testing.T) {
			f := fixture()
			delete(f.workflow, field)
			err := monitor(context.Background(), f, &fakeClock{epoch}, sample)
			if err == nil || !strings.Contains(err.Error(), "UNSUPERVISED") || f.cancelCount != 0 || len(f.calls) != 2 {
				t.Fatalf("err=%v cancels=%d calls=%v", err, f.cancelCount, f.calls)
			}
		})
	}
}

func TestIncompleteConfirmationIsBounded(t *testing.T) {
	for _, mode := range []string{"recovers", "persistent", "contradictory"} {
		t.Run(mode, func(t *testing.T) {
			f := fixture()
			reads := 0
			f.onCall = func(path string) {
				if path == "api/v2/workflow/"+sample.Workflow && f.cancelCount > 0 {
					reads++
					delete(f.workflow, "id")
					if mode == "recovers" && reads > 1 {
						f.workflow["id"] = sample.Workflow
					}
					if mode == "contradictory" {
						f.workflow["pipeline_id"] = "contradictory"
					}
				}
			}
			c := &fakeClock{epoch}
			err := monitor(context.Background(), f, c, sample)
			if err == nil || f.cancelCount != 1 {
				t.Fatalf("err=%v cancels=%d", err, f.cancelCount)
			}
			switch mode {
			case "recovers":
				if !strings.Contains(err.Error(), "jobs stopped") || c.now.Sub(epoch) != pollInterval {
					t.Fatalf("err=%v elapsed=%v", err, c.now.Sub(epoch))
				}
			case "persistent":
				if !strings.Contains(err.Error(), "UNCONFIRMED") || c.now.Sub(epoch) != confirmationLimit {
					t.Fatalf("err=%v elapsed=%v", err, c.now.Sub(epoch))
				}
			case "contradictory":
				if !strings.Contains(err.Error(), "UNCONFIRMED") || !strings.Contains(err.Error(), "manually") || c.now.Sub(epoch) != 0 {
					t.Fatalf("err=%v elapsed=%v", err, c.now.Sub(epoch))
				}
			}
		})
	}
}
