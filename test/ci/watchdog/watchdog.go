// Package main provides an operator-run guard for an explicitly approved CircleCI workflow.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	jobLimit          = 15 * time.Minute
	sessionLimit      = 45 * time.Minute
	pollInterval      = 10 * time.Second
	confirmationLimit = time.Minute
	maxClockSkew      = 30 * time.Second
)

type target struct {
	Project, Pipeline, Workflow, Branch, Revision string
}

type api interface {
	Call(context.Context, string, string) (json.RawMessage, error)
}

type clock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

func read(ctx context.Context, client api, path string, out any) error {
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := client.Call(callCtx, "GET", path)
	if err != nil {
		return fmt.Errorf("CircleCI read failed: %w", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("invalid CircleCI JSON response")
	}
	return nil
}

func identify(ctx context.Context, client api, want target) (workflowInfo, error) {
	var w workflowInfo
	if err := want.validate(); err != nil {
		return w, err
	}
	var p pipelineInfo
	if err := read(ctx, client, "api/v2/pipeline/"+want.Pipeline, &p); err != nil {
		return w, err
	}
	if err := p.validate(want); err != nil {
		return w, err
	}
	if err := read(ctx, client, "api/v2/workflow/"+want.Workflow, &w); err != nil {
		return w, err
	}
	return w, w.validate(want)
}

func unsupervised(want target, err error) error {
	return fmt.Errorf("UNSUPERVISED: workflow %s may still be running; check the target workflow manually; %w", want.Workflow, err)
}

// readBoundWorkflow is only used after initial identity verification. Missing
// fields may be transient, but all attempts share one normal request budget.
func readBoundWorkflow(ctx context.Context, client api, want target) (workflowInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		var w workflowInfo
		if err := read(ctx, client, "api/v2/workflow/"+want.Workflow, &w); err != nil {
			return w, err
		}
		err := w.validate(want)
		if !errors.Is(err, errIncompleteIdentity) {
			return w, err
		}
	}
	return workflowInfo{}, errIncompleteIdentity
}

// monitor cannot launch, approve or rerun anything. The caller must separately
// authorize cancellation. Identity must be established before any mutation.
func monitor(ctx context.Context, client api, timer clock, want target) error {
	w, err := identify(ctx, client, want)
	if err != nil {
		return unsupervised(want, err)
	}
	began := timer.Now()
	seen := jobTracker{starts: make(map[string]observedStart), ids: make(map[string]string)}
	for {
		if terminal(w.Status) && w.Status != "success" {
			return fmt.Errorf("workflow terminal=%s; coverage did not pass", w.Status)
		}
		if w.Status != "running" && w.Status != "on_hold" && w.Status != "success" {
			return cancelAndConfirm(client, timer, want, "unknown workflow status")
		}
		var jobs jobPage
		err := read(ctx, client, "api/v2/workflow/"+want.Workflow+"/job", &jobs)
		passed := false
		if err == nil {
			passed, err = jobs.inspect(want, timer.Now(), seen)
		}
		if err != nil {
			if terminal(w.Status) {
				return fmt.Errorf("terminal workflow lacks valid coverage: %w", err)
			}
			return cancelAndConfirm(client, timer, want, err.Error())
		}
		if w.Status == "success" {
			if !passed {
				return errors.New("workflow success without all required job passes")
			}
			return nil
		}
		if timer.Now().Sub(began) >= sessionLimit {
			return cancelAndConfirm(client, timer, want, "45-minute supervisor session deadline")
		}
		if err := timer.Sleep(ctx, pollInterval); err != nil {
			return cancelAndConfirm(client, timer, want, "supervisor interrupted")
		}
		w, err = readBoundWorkflow(ctx, client, want)
		if errors.Is(err, errWorkflowIdentity) {
			// A contradictory response is not permission to cancel a target.
			return unsupervised(want, err)
		}
		if err != nil {
			// Initial verification bound this session to want, never to a value
			// supplied by the incomplete response.
			return cancelAndConfirm(client, timer, want, err.Error())
		}
	}
}

func cancelAndConfirm(client api, timer clock, want target, reason string) error {
	// Cleanup uses a separate bounded context, including after SIGINT/SIGTERM.
	ctx, done := context.WithTimeout(context.Background(), confirmationLimit)
	defer done()
	began := timer.Now()
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_, requestErr := client.Call(callCtx, "POST", "api/v2/workflow/"+want.Workflow+"/cancel")
	cancel()
	// Never repeat an uncertain POST. A failed response may still have applied.
	for {
		var w workflowInfo
		if read(ctx, client, "api/v2/workflow/"+want.Workflow, &w) == nil {
			validationErr := w.validate(want)
			if errors.Is(validationErr, errWorkflowIdentity) {
				return fmt.Errorf("UNCONFIRMED cancellation: %s; check target workflow manually; %w", reason, validationErr)
			}
			if validationErr == nil && terminal(w.Status) {
				var jobs jobPage
				if read(ctx, client, "api/v2/workflow/"+want.Workflow+"/job", &jobs) == nil && jobs.stopped(want) {
					return fmt.Errorf("%s; terminal=%s and jobs stopped after cancellation request (request acknowledged=%t); not successful coverage", reason, w.Status, requestErr == nil)
				}
			}
		}
		if ctx.Err() != nil || timer.Now().Sub(began) >= confirmationLimit {
			break
		}
		if timer.Sleep(ctx, pollInterval) != nil {
			break
		}
	}
	return fmt.Errorf("UNCONFIRMED cancellation: %s; check target workflow manually; no repeated POST", reason)
}
