package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0874: verifiable preconditions of a runbook step, and the read-only
// preflight that checks them. A runbook is markdown, so the per-step field is a
// declared line in the body (outside code fences):
//
//	Pré-condição S11: registro F-0869        the entity exists in the backend
//	Pré-condição S11: result F-0869          the feature has a result with content
//	Pré-condição S11: mr rockt/hub!2150 merged   the MR is in that state on the forge
//	Pré-condição S11: shell verde em produção     anything else: manual
//
// The step is the S<n> token on the line itself; without one the precondition
// holds for every step. What the cerne cannot check is "manual", never met.

// Precondition statuses.
const (
	PreMet     = "met"
	PrePending = "pending"
	PreManual  = "manual"
)

// Precondition and the parser live in the store, shared with the lint.
type Precondition = store.Precondition

// ParsePreconditions: see store.ParsePreconditions.
var ParsePreconditions = store.ParsePreconditions

// PreResult is a precondition with the outcome of its check.
type PreResult struct {
	Precondition
	Status string
	Detail string
}

// MRState asks the forge for the state of a merge request ("opened", "merged",
// "closed"). It is a variable so tests and other forges can replace it; the
// default asks glab, read-only, with a deadline.
var MRState = glabMRState

func glabMRState(ctx context.Context, project, iid string) (string, error) {
	if !store.ValidMRRef(project, iid) {
		return "", fmt.Errorf("invalid MR reference %q!%q", project, iid)
	}
	// "--" ends the flags: the positional can never be read as one (F-0874).
	cmd := exec.CommandContext(ctx, "glab", "mr", "view", "-R", project, "-F", "json", "--", iid)
	// The deadline kills glab, not a child (credential helper, pager) still
	// holding stdout: without WaitDelay, Output waits for that child (F-0886).
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	// glab exited 0 and its answer is in out; only a child kept the pipe open.
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	if err != nil {
		return "", fmt.Errorf("glab: %w", err)
	}
	var v struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(out, &v); err != nil || v.State == "" {
		return "", errors.New("glab returned no state")
	}
	return strings.ToLower(v.State), nil
}

func validMRArg(a string) bool {
	proj, iid, _ := strings.Cut(a, "!")
	return store.ValidMRRef(proj, iid)
}

// checkableMR is an MR precondition the forge can answer: a "manual" result of
// one means the forge did not answer (state unknown), not that the cerne
// cannot verify it.
func checkableMR(p Precondition) bool {
	return p.Problem == "" && p.Kind == "mr" && len(p.Args) == 2 && validMRArg(p.Args[0]) && store.NormMRState(p.Args[1]) != ""
}

// CheckPreconditions checks each precondition against the backend (and the
// forge for MRs). Read-only. A kind the cerne cannot verify, and a forge that
// cannot be reached, are "manual": never "met".
func CheckPreconditions(repoDir string, pcs []Precondition) []PreResult {
	return CheckPreconditionsWith(repoDir, pcs, NewForge())
}

// Forge answers MR states for one invocation: each MR is asked once (errors
// too) and the forge gets a total time budget; past it the MRs are not asked
// and Cut is set, so the warning says the ceiling cut the check.
type Forge struct {
	Budget      time.Duration
	CallTimeout time.Duration
	Cut         bool
	spent       time.Duration
	cache       map[string]forgeAnswer
}

type forgeAnswer struct {
	state string
	err   error
}

// NewForge: 10 s in total, one call at most 5 s. A call gets no more than what
// is left of the total, so 10 s is a ceiling, not a start-of-call check.
func NewForge() *Forge {
	return &Forge{Budget: 10 * time.Second, CallTimeout: 5 * time.Second, cache: map[string]forgeAnswer{}}
}

func (f *Forge) state(project, iid string) (string, error) {
	if f.cache == nil {
		f.cache = map[string]forgeAnswer{}
	}
	k := project + "!" + iid
	if a, ok := f.cache[k]; ok {
		return a.state, a.err
	}
	left := f.Budget - f.spent
	if left <= 0 {
		f.Cut = true
		return "", fmt.Errorf("forge time budget of %s spent by earlier MRs", f.Budget)
	}
	d := min(left, f.CallTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	t0 := time.Now()
	st, err := MRState(ctx, project, iid)
	f.spent += time.Since(t0)
	// The ceiling, not the forge, ended this call: its deadline was what was
	// left, and that deadline is what stopped it — a forge failing on its own
	// in the last call is not a cut (F-0886).
	if err != nil && d == left && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		f.Cut = true
	}
	f.cache[k] = forgeAnswer{st, err}
	return st, err
}

// CheckPreconditionsWith is CheckPreconditions sharing a Forge (cache and
// budget) across calls, e.g. every run of one `cerne next`.
func CheckPreconditionsWith(repoDir string, pcs []Precondition, forge *Forge) []PreResult {
	out := make([]PreResult, 0, len(pcs))
	for _, p := range pcs {
		r := PreResult{Precondition: p, Status: PreManual, Detail: "the cerne cannot verify this: check it by hand"}
		switch {
		case p.Problem != "":
			r.Detail = "invalid declaration (" + p.Problem + "): never run, check it by hand"
		case p.Kind == "registro" && len(p.Args) == 1 && store.ValidEntityID(p.Args[0]):
			if store.EntityExists(repoDir, p.Args[0]) {
				r.Status, r.Detail = PreMet, p.Args[0]+" exists"
			} else {
				r.Status, r.Detail = PrePending, p.Args[0]+" not found in the backend"
			}
		case p.Kind == "result" && len(p.Args) == 1 && store.ValidFeatureID(p.Args[0]):
			data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", p.Args[0], "result.md"))
			if err == nil && doccheck.HasRealContent(data) {
				r.Status, r.Detail = PreMet, p.Args[0]+" has a result"
			} else {
				r.Status, r.Detail = PrePending, p.Args[0]+" has no result written"
			}
		case checkableMR(p):
			proj, iid, _ := strings.Cut(p.Args[0], "!")
			want := store.NormMRState(p.Args[1])
			got, err := forge.state(proj, iid)
			switch {
			case err != nil:
				r.Detail = "forge not reachable (" + err.Error() + "): check it by hand"
			case store.NormMRState(got) == want:
				r.Status, r.Detail = PreMet, p.Args[0]+" is "+want
			default:
				r.Status, r.Detail = PrePending, p.Args[0]+" is "+store.NormMRState(got)+", wanted "+want
			}
		}
		out = append(out, r)
	}
	return out
}

// preconditionsUpTo keeps the ones that hold for step upTo: no step, or a step
// not after it.
func preconditionsUpTo(pcs []Precondition, upTo string) []Precondition {
	var out []Precondition
	for _, p := range pcs {
		if p.Step == "" || store.CompareSteps(p.Step, upTo) <= 0 {
			out = append(out, p)
		}
	}
	return out
}

// Preflight lists the preconditions of runbook rbID up to step upTo ("" = the
// last step) with their status. Changes nothing.
func Preflight(repoDir, rbID, upTo string) ([]PreResult, string, error) {
	return PreflightWith(repoDir, rbID, upTo, NewForge())
}

// PreflightWith is Preflight asking the forge through forge, so the caller
// reads its Cut afterwards (F-0886: 'runbook preflight --json').
func PreflightWith(repoDir, rbID, upTo string, forge *Forge) ([]PreResult, string, error) {
	if _, err := store.LoadRunbook(repoDir, rbID); errors.Is(err, store.ErrNotFound) {
		return nil, "", fmt.Errorf("runbook %s not found in the backend", rbID)
	} else if err != nil {
		return nil, "", err
	}
	body, err := store.LoadRunbookContent(repoDir, rbID)
	if err != nil {
		return nil, "", err
	}
	steps := store.RunbookSteps(body)
	if upTo == "" && len(steps) > 0 {
		upTo = steps[len(steps)-1]
	}
	if upTo != "" && !slices.Contains(steps, upTo) {
		return nil, "", fmt.Errorf("step %q is not named in %s (steps: %s)", upTo, rbID, strings.Join(steps, " "))
	}
	pcs := ParsePreconditions(body)
	if upTo != "" {
		pcs = preconditionsUpTo(pcs, upTo)
	}
	return CheckPreconditionsWith(repoDir, pcs, forge), upTo, nil
}

// RunPreflight is the preflight of a run in progress: the preconditions of its
// next step (the first with no ok) and the global ones, minus the steps already
// done. ok is false when the run has no step left.
func RunPreflight(repoDir string, run store.Run, forge *Forge) (next string, res []PreResult, ok bool, err error) {
	for _, s := range run.Steps {
		if !runStepOK(run, s) {
			next = s
			break
		}
	}
	if next == "" {
		return "", nil, false, nil
	}
	body, err := store.LoadRunbookContent(repoDir, run.Runbook)
	if err != nil {
		return "", nil, false, err
	}
	var pcs []Precondition
	for _, p := range preconditionsUpTo(ParsePreconditions(body), next) {
		if p.Step == "" || !runStepOK(run, p.Step) {
			pcs = append(pcs, p)
		}
	}
	if forge == nil {
		forge = NewForge()
	}
	res = CheckPreconditionsWith(repoDir, pcs, forge)
	// A manual precondition with no step matters before the run starts: once a
	// step is ok, repeating it on every step only buries the rest (F-0886).
	if slices.ContainsFunc(run.Steps, func(s string) bool { return runStepOK(run, s) }) {
		res = slices.DeleteFunc(res, func(r PreResult) bool {
			return r.Step == "" && r.Status == PreManual && !checkableMR(r.Precondition)
		})
	}
	return next, res, true, nil
}

// OpenPreconditions is the subset of results that are not met.
func OpenPreconditions(res []PreResult) []PreResult {
	var out []PreResult
	for _, r := range res {
		if r.Status != PreMet {
			out = append(out, r)
		}
	}
	return out
}
