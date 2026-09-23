package main

import (
	"context"
	"errors"
	"reflect"
	"time"
)

// reusedLinux is session-local evidence, never a replacement for current state.
// Only the successful Linux prefix can be reused; Windows must execute anew.
type reusedLinux struct{ approval, build jobInfo }

func loadReusedLinux(ctx context.Context, client api, want target) (*reusedLinux, error) {
	if want.SourceWorkflow == "" {
		return nil, nil
	}
	// All source identity and graph reads share one budget, not one per request.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	source := want
	source.Workflow, source.SourceWorkflow = want.SourceWorkflow, ""
	w, err := identify(ctx, client, source)
	if err != nil {
		return nil, err
	}
	if !terminal(w.Status) {
		return nil, errors.New("source workflow is not terminal")
	}
	var page jobPage
	if err := read(ctx, client, "api/v2/workflow/"+source.Workflow+"/job", &page); err != nil {
		return nil, err
	}
	if !page.stopped(source) {
		return nil, errors.New("source job graph incomplete or not stopped")
	}
	var evidence reusedLinux
	for _, j := range page.Items {
		switch j.Name {
		case jobNames[0]:
			evidence.approval = j
		case jobNames[1]:
			evidence.build = j
		}
	}
	a, b := evidence.approval, evidence.build
	if a.Type != "approval" || b.Type != "build" || a.Status != "success" || b.Status != "success" ||
		len(a.Dependencies) != 0 || len(b.Dependencies) != 1 || b.Dependencies[0] != a.ID {
		return nil, errors.New("source Linux prefix lacks successful approval dependency")
	}
	for _, j := range []jobInfo{a, b} {
		if j.Requires == nil {
			continue
		}
		if len(j.Requires) != len(j.Dependencies) {
			return nil, errors.New("source dependency result rules mismatch")
		}
		for _, dep := range j.Dependencies {
			outcomes := j.Requires[dep]
			if len(outcomes) != 1 || outcomes[0] != "success" {
				return nil, errors.New("source dependency results widened")
			}
		}
	}
	if b.Started == "" || b.Stopped == "" {
		return nil, errors.New("source Linux timing evidence missing")
	}
	return &evidence, nil
}

func (e *reusedLinux) corroborate(page jobPage) (jobPage, error) {
	if e == nil {
		return page, nil
	}
	// Work on a copy: rejected evidence must not partially modify caller state.
	out := page
	out.Items = append([]jobInfo(nil), page.Items...)
	for _, original := range []jobInfo{e.approval, e.build} {
		matches := 0
		for i, j := range out.Items {
			if j.Name != original.Name {
				continue
			}
			matches++
			if j.ID != original.ID || j.Project != original.Project || j.Type != original.Type ||
				j.Status != "success" || !reflect.DeepEqual(j.Dependencies, original.Dependencies) {
				return page, errors.New("reused Linux identity/status/dependency mismatch")
			}
			if j.Type != "build" {
				continue
			}
			if j.Started != "" && j.Started != original.Started || j.Stopped != "" && j.Stopped != original.Stopped {
				return page, errors.New("reused Linux timestamp contradicts source")
			}
			out.Items[i].Started, out.Items[i].Stopped = original.Started, original.Stopped
		}
		if matches != 1 {
			return page, errors.New("reused Linux prefix missing or duplicated")
		}
	}
	// inspect still validates the entire current graph and original duration.
	return out, nil
}

func inspectJobs(ctx context.Context, client api, want target, timer clock, seen jobTracker, evidence *reusedLinux) (bool, error) {
	var page jobPage
	if err := read(ctx, client, "api/v2/workflow/"+want.Workflow+"/job", &page); err != nil {
		return false, err
	}
	page, err := evidence.corroborate(page)
	if err != nil {
		return false, err
	}
	return page.inspect(want, timer.Now(), seen)
}
