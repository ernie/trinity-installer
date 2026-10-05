package install

import (
	"context"
	"fmt"

	"github.com/ernie/trinity-installer/internal/patch"
	"github.com/ernie/trinity-installer/internal/quake3"
	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/target"
)

type State int

const (
	Waiting State = iota
	Running
	Done
	Failed
)

type Progress struct {
	Index int
	Step  target.Step
	State State
	Line  string
}

type Options struct {
	Fetch     func(ctx context.Context, spec release.Spec, log func(string)) ([]byte, error)
	Paks      []quake3.Pak
	PatchRels []string
	Patch     *patch.Set
	Art       map[string][]byte
	// Carry keeps what finished steps produced, so a retry may resume after a reconnect.
	Carry *Carry
}

type Carry struct{ st *state }

type StepError struct {
	Index int
	Step  target.Step
	Err   error
}

func (e *StepError) Error() string { return fmt.Sprintf("%s: %v", e.Step, e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

type state struct {
	opts    Options
	pkg     *release.Package
	paksDir string
	appID   uint32
}

// Run performs plan[from:] against t; a retry (from > 0) first asks the target to reconnect.
func Run(ctx context.Context, t target.Target, plan []target.Step, opts Options, from int, report func(Progress)) error {
	if from < 0 || from >= len(plan) {
		return fmt.Errorf("no step %d in a plan of %d", from, len(plan))
	}
	var st *state
	if from > 0 {
		// A resumed step needs what the earlier steps produced; without it, it would run on an empty state.
		if opts.Carry == nil || opts.Carry.st == nil {
			return fmt.Errorf("cannot resume at %s without the earlier steps' state", plan[from])
		}
		st = opts.Carry.st
	} else {
		st = &state{}
		if opts.Carry != nil {
			opts.Carry.st = st
		}
	}
	st.opts = opts
	if from > 0 {
		if err := t.Reconnect(ctx); err != nil {
			return &StepError{Index: from, Step: plan[from], Err: err}
		}
	}
	for i := from; i < len(plan); i++ {
		step := plan[i]
		report(Progress{Index: i, Step: step, State: Running})
		log := func(line string) { report(Progress{Index: i, Step: step, State: Running, Line: line}) }
		if err := run(ctx, t, st, step, log); err != nil {
			report(Progress{Index: i, Step: step, State: Failed, Line: err.Error()})
			return &StepError{Index: i, Step: step, Err: err}
		}
		report(Progress{Index: i, Step: step, State: Done})
	}
	return nil
}

func run(ctx context.Context, t target.Target, st *state, step target.Step, log func(string)) error {
	switch step {
	case target.FetchRelease:
		return fetchRelease(ctx, t, st, log)
	case target.PrepareDestination:
		dir, err := t.PrepareDestination(ctx, log)
		st.paksDir = dir
		return err
	case target.PushPackage:
		return t.PushPackage(ctx, st.pkg, log)
	case target.PushRetailPaks:
		return pushPaks(ctx, t.Store(), st, log)
	case target.PushPatch:
		return pushPatch(ctx, t.Store(), st, log)
	case target.RegisterLaunchEntry:
		return t.RegisterLaunchEntry(ctx, log)
	case target.ReadAppID:
		id, err := t.ReadAppID(ctx, log)
		st.appID = id
		return err
	case target.RegisterVR:
		return t.RegisterVR(ctx, st.appID, st.opts.Art, log)
	case target.InstallArtwork:
		return t.InstallArtwork(ctx, st.appID, st.opts.Art, log)
	}
	return fmt.Errorf("unknown step %d", step)
}
