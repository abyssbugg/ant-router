package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClockSkewBoundaries(t *testing.T) {
	for _, completed := range []bool{false, true} {
		for _, skew := range []time.Duration{0, 30 * time.Second, 30*time.Second + time.Nanosecond} {
			name := "running/" + skew.String()
			if completed {
				name = "completed/" + skew.String()
			}
			t.Run(name, func(t *testing.T) {
				f := fixture()
				f.jobs[1]["started_at"] = epoch.Add(skew).Format(time.RFC3339Nano)
				if completed {
					for _, job := range f.jobs {
						job["status"] = "success"
						if job["type"] == "build" {
							job["started_at"] = epoch.Add(-time.Minute).Format(time.RFC3339Nano)
							job["stopped_at"] = epoch.Add(skew).Format(time.RFC3339Nano)
						}
					}
				}
				var jobs jobPage
				raw, err := json.Marshal(map[string]any{"items": f.jobs})
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, &jobs); err != nil {
					t.Fatal(err)
				}
				seen := jobTracker{starts: make(map[string]observedStart), ids: make(map[string]string)}
				passed, err := jobs.inspect(sample, epoch, seen)
				if skew <= 30*time.Second {
					if err != nil || passed != completed {
						t.Fatalf("passed=%t err=%v", passed, err)
					}
				} else if err == nil || passed {
					t.Fatalf("out-of-bound skew accepted: passed=%t err=%v", passed, err)
				}
			})
		}
	}
}

func TestFutureStartCannotExtendObservedJobDeadline(t *testing.T) {
	f := fixture()
	f.jobs[1]["started_at"] = epoch.Add(30 * time.Second).Format(time.RFC3339Nano)
	c := &fakeClock{epoch}
	err := monitor(context.Background(), f, c, sample)
	if err == nil || !strings.Contains(err.Error(), "15-minute deadline") || f.cancelCount != 1 || c.now.Sub(epoch) != jobLimit {
		t.Fatalf("err=%v cancels=%d elapsed=%v", err, f.cancelCount, c.now.Sub(epoch))
	}
}

func TestSkewDoesNotRelaxCompletedDurationLimit(t *testing.T) {
	f := fixture()
	f.workflow["status"] = "success"
	for _, job := range f.jobs {
		job["status"] = "success"
		if job["type"] == "build" {
			job["started_at"] = epoch.Add(30*time.Second - jobLimit).Format(time.RFC3339Nano)
			job["stopped_at"] = epoch.Add(30 * time.Second).Format(time.RFC3339Nano)
		}
	}
	err := monitor(context.Background(), f, &fakeClock{epoch}, sample)
	if err == nil || !strings.Contains(err.Error(), "completed job duration") || f.cancelCount != 0 {
		t.Fatalf("err=%v cancels=%d", err, f.cancelCount)
	}
}
