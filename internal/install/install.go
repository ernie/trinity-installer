package install

import (
	"context"
	"fmt"

	"github.com/ernie/trinity-installer/internal/frame"
	"github.com/ernie/trinity-installer/internal/quake3"
)

type State int

const (
	Waiting State = iota
	Running
	Done
	Failed
)

type Progress struct {
	Step  int
	Name  string
	State State
	Line  string
}

type Options struct {
	GameID       string
	Fetch        func(ctx context.Context, log func(string)) ([]byte, error)
	Paks         []quake3.Pak
	Art          map[string][]byte
	ResponsePath func() string
	// Carry keeps what finished steps produced, so a retry may run on a new session.
	Carry *Carry
}

// Carry is the handle that moves a run's state between sessions.
type Carry struct{ st *state }

var StepNames = []string{
	"Fetch release",
	"Prepare title",
	"Push package",
	"Push retail paks",
	"Register shortcut",
	"Read app id",
	"Register with SteamVR",
	"Install artwork",
}

type StepError struct {
	Step int
	Err  error
}

func (e *StepError) Error() string { return fmt.Sprintf("%s: %v", StepNames[e.Step], e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

// state carries what earlier steps produced; Retry from a step reuses it.
type state struct {
	opts     Options
	sess     frame.Session
	titleDir string
	zip      []byte
	userID   string
	appID    uint32
}

type stepFunc func(ctx context.Context, st *state, log func(string)) error

func steps() []stepFunc {
	return []stepFunc{fetchRelease, prepareTitle, pushPackage, pushPaks, registerShortcut, readAppID, registerManifest, installArt}
}

var runs = map[frame.Session]*state{}

// Run executes the steps from index from; a retry reuses the state in opts.Carry, or the previous run's on the same session when Carry is nil.
func Run(ctx context.Context, sess frame.Session, opts Options, from int, report func(Progress)) error {
	var st *state
	if opts.Carry != nil {
		st = opts.Carry.st
	} else {
		st = runs[sess]
	}
	if st == nil || from == 0 {
		st = &state{}
		if opts.Carry != nil {
			opts.Carry.st = st
		} else {
			runs[sess] = st
		}
	}
	st.opts, st.sess = opts, sess
	if st.opts.GameID == "" {
		st.opts.GameID = "Trinity"
	}
	if st.opts.ResponsePath == nil {
		st.opts.ResponsePath = defaultResponsePath
	}
	all := steps()
	for i := from; i < len(all); i++ {
		name := StepNames[i]
		report(Progress{Step: i, Name: name, State: Running})
		log := func(line string) { report(Progress{Step: i, Name: name, State: Running, Line: line}) }
		if err := all[i](ctx, st, log); err != nil {
			report(Progress{Step: i, Name: name, State: Failed, Line: err.Error()})
			return &StepError{Step: i, Err: err}
		}
		report(Progress{Step: i, Name: name, State: Done})
	}
	return nil
}
