package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// Precondition is one declared line.
type Precondition struct {
	Step string // "" = every step
	Kind string // "result" | "registro" | "mr" | "manual"
	Args []string
	Text string // what the author wrote after the colon
}

// PreResult is a precondition with the outcome of its check.
type PreResult struct {
	Precondition
	Status string
	Detail string
}

var preLineRe = regexp.MustCompile(`(?i)^\s*(?:[-*+>]\s*)*(?:\*\*)?pr[eé]-?condi[cç][aã]o(?:\*\*)?\s*(?:\(?\s*((?-i:S\d+[a-z]?(?:\.\d+)*))\s*\)?)?\s*:\s*(?:\*\*)?\s*(.+?)\s*$`)

// ParsePreconditions reads the declared preconditions of a runbook body, in
// order, skipping code fences.
func ParsePreconditions(body string) []Precondition {
	var out []Precondition
	fence := false
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		m := preLineRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		p := Precondition{Step: m[1], Text: m[2], Kind: "manual"}
		f := strings.Fields(strings.Trim(m[2], "*"))
		for i := range f {
			f[i] = strings.Trim(f[i], "`.,;")
		}
		switch {
		case len(f) == 2 && strings.EqualFold(f[0], "result"):
			p.Kind, p.Args = "result", f[1:]
		case len(f) == 2 && strings.EqualFold(f[0], "registro"):
			p.Kind, p.Args = "registro", f[1:]
		case len(f) == 3 && strings.EqualFold(f[0], "mr") && strings.Contains(f[1], "!"):
			p.Kind, p.Args = "mr", f[1:]
		}
		out = append(out, p)
	}
	return out
}

// MRState asks the forge for the state of a merge request ("opened", "merged",
// "closed"). It is a variable so tests and other forges can replace it; the
// default asks glab, read-only, with a deadline.
var MRState = glabMRState

func glabMRState(project, iid string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "glab", "mr", "view", iid, "-R", project, "-F", "json").Output()
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

func normMRState(s string) string {
	switch strings.ToLower(s) {
	case "aberta", "aberto", "open", "opened":
		return "opened"
	case "mergeada", "mergeado", "merged":
		return "merged"
	case "fechada", "fechado", "closed":
		return "closed"
	}
	return strings.ToLower(s)
}

// CheckPreconditions checks each precondition against the backend (and the
// forge for MRs). Read-only. A kind the cerne cannot verify, and a forge that
// cannot be reached, are "manual": never "met".
func CheckPreconditions(repoDir string, pcs []Precondition) []PreResult {
	out := make([]PreResult, 0, len(pcs))
	for _, p := range pcs {
		r := PreResult{Precondition: p, Status: PreManual, Detail: "the cerne cannot verify this: check it by hand"}
		switch p.Kind {
		case "registro":
			if store.EntityExists(repoDir, p.Args[0]) {
				r.Status, r.Detail = PreMet, p.Args[0]+" exists"
			} else {
				r.Status, r.Detail = PrePending, p.Args[0]+" not found in the backend"
			}
		case "result":
			data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", p.Args[0], "result.md"))
			if err == nil && doccheck.HasRealContent(data) {
				r.Status, r.Detail = PreMet, p.Args[0]+" has a result"
			} else {
				r.Status, r.Detail = PrePending, p.Args[0]+" has no result written"
			}
		case "mr":
			proj, iid, _ := strings.Cut(p.Args[0], "!")
			want := normMRState(p.Args[1])
			got, err := MRState(proj, iid)
			switch {
			case err != nil:
				r.Detail = "forge not reachable (" + err.Error() + "): check it by hand"
			case normMRState(got) == want:
				r.Status, r.Detail = PreMet, p.Args[0]+" is "+want
			default:
				r.Status, r.Detail = PrePending, p.Args[0]+" is "+normMRState(got)+", wanted "+want
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
	return CheckPreconditions(repoDir, pcs), upTo, nil
}

// RunPreflight is the preflight of a run in progress: the preconditions of its
// next step (the first with no ok) and the global ones, minus the steps already
// done. ok is false when the run has no step left.
func RunPreflight(repoDir string, run store.Run) (next string, res []PreResult, ok bool, err error) {
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
	return next, CheckPreconditions(repoDir, pcs), true, nil
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
