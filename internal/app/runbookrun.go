package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0873: the execution of a runbook window as a cerne record. The run is a
// Work (so `work list` and `show W-N` see it, N-0154) plus
// content/runs/<W-id>.json with the steps and what was recorded against them.

// Run entry states.
const (
	RunOK   = "ok"
	RunPare = "pare"
)

// brt is Brasília time, fixed at UTC-3 (no DST since 2019), so the record does
// not depend on the tz database of the machine that wrote it.
var brt = time.FixedZone("BRT", -3*60*60)

var (
	// emailRe: the first label after @ holds a letter and the TLD is alphabetic,
	// so next@15.0.1, pkg@1.2.3 and ./build@2.0.log (versions) are not e-mails.
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]*[A-Za-z][A-Za-z0-9-]*(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	// personIDRe: a UUID or a CPF, the ids of a person that must not reach the
	// backend. Limit: an employee/registration number ("matricula 12345") has no
	// format to tell it from a ticket or a pid, so it is not refused.
	personIDRe = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b|\b\d{3}\.\d{3}\.\d{3}-\d{2}\b`)
	stepIDRe   = regexp.MustCompile(`(?i)^S\d+[a-z]?(\.\d+)*$`)
)

// refusePersonal refuses an e-mail or a person id in a free-text flag. It is
// the single check for every flag that reaches the record.
func refusePersonal(flag, v string) error {
	if emailRe.MatchString(v) || personIDRe.MatchString(v) {
		return fmt.Errorf("%s: no e-mail or person id in the record: cite a nickname only", flag)
	}
	return nil
}

// RunStepInput is one `runbook step` call.
type RunStepInput struct {
	Step     string
	Pare     bool   // false = ok
	Word     string // the human's literal word (ok)
	Output   string // the exit that stopped the run (pare)
	By       string // nickname of the human who said it
	Expected string // overrides the run's expected word for this step
	Evidence string // path of a local file; only its path and sha256 are kept
}

// HashEvidence validates that evidence is the path of an existing regular file
// and returns its sha256. The content is hashed and dropped, never stored: it
// is production log. Anything that is not a path (multi-line, NUL) is refused.
func HashEvidence(path string) (store.RunEvidence, error) {
	if strings.ContainsAny(path, "\r\n\x00") {
		return store.RunEvidence{}, fmt.Errorf("--evidencia takes the path of a file, not its content; the content is never stored")
	}
	// A UUID in a path (pod dirs, dumps) is legitimate; an e-mail in it is not.
	if emailRe.MatchString(path) {
		return store.RunEvidence{}, fmt.Errorf("--evidencia: no e-mail in the record: cite a nickname only")
	}
	st, err := os.Stat(path)
	if err != nil {
		return store.RunEvidence{}, fmt.Errorf("--evidencia must be the path of an existing file (%v); the content is never stored", err)
	}
	if !st.Mode().IsRegular() {
		return store.RunEvidence{}, fmt.Errorf("--evidencia %q is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return store.RunEvidence{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return store.RunEvidence{}, err
	}
	return store.RunEvidence{Path: path, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// StartRun opens the execution record of runbook rbID for a window: a new
// active Work (refs: the runbook) plus its run file. steps empty = extracted
// from the runbook body (store.RunbookSteps); expected is the word the owner is
// expected to say at each step (optional).
func StartRun(repoDir, projDir, rbID, window string, steps []string, expected, agent string) (store.Work, store.Run, error) {
	window = strings.TrimSpace(window)
	if window == "" {
		return store.Work{}, store.Run{}, fmt.Errorf("--janela is required: the label of the window (e.g. J3)")
	}
	for flag, v := range map[string]string{"--janela": window, "--palavra": expected, "--passos": strings.Join(steps, ",")} {
		if err := refusePersonal(flag, v); err != nil {
			return store.Work{}, store.Run{}, err
		}
	}
	seen := map[string]bool{}
	for _, s := range steps {
		if !stepIDRe.MatchString(s) {
			return store.Work{}, store.Run{}, fmt.Errorf("--passos: %q is not a step id (S<n>, e.g. S3, S6.6)", s)
		}
		if seen[s] {
			return store.Work{}, store.Run{}, fmt.Errorf("--passos: step %s is listed twice", s)
		}
		seen[s] = true
	}
	actor := strings.TrimSpace(agent)
	if actor == "" {
		actor = defaultWorkActor()
	}
	var w store.Work
	var run store.Run
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		rb, err := store.LoadRunbook(repoDir, rbID)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("runbook %s not found in the backend", rbID)
		}
		if err != nil {
			return "", err
		}
		if rb.State != domain.RunbookActive {
			return "", fmt.Errorf("runbook %s is %s: only an active runbook can be run", rbID, rb.State)
		}
		list := steps
		inferred := len(list) == 0
		if inferred {
			body, err := store.LoadRunbookContent(repoDir, rbID)
			if err != nil {
				return "", err
			}
			list = store.RunbookSteps(body)
		}
		if len(list) == 0 {
			return "", fmt.Errorf("%s names no step (S<n> tokens): pass --passos S1,S2,...", rbID)
		}
		works, err := store.LoadAllWorks(repoDir)
		if err != nil {
			return "", err
		}
		for _, o := range works {
			if o.State != domain.WorkActive || !slices.Contains(o.Refs, rbID) {
				continue
			}
			if r, err := store.LoadRun(repoDir, o.ID); err == nil && r.Window == window {
				return "", fmt.Errorf("%s already records %s window %s (active): record steps there, or 'cerne work done %s'", o.ID, rbID, window, o.ID)
			}
		}
		id, err := store.NextID(repoDir, "W")
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		title := fmt.Sprintf("Execução de %s — janela %s", rbID, window)
		w = store.Work{ID: id, Title: title, State: domain.WorkActive, Actor: actor, Refs: []string{rbID}, CreatedAt: now, UpdatedAt: now}
		run = store.Run{ID: id, Runbook: rbID, Window: window, Expected: strings.TrimSpace(expected), Steps: list, Explicit: !inferred, State: store.RunOpen}
		if err := store.SaveWork(repoDir, w); err != nil {
			return "", err
		}
		if err := store.SaveRun(repoDir, run); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "work.started", map[string]string{"title": title, "actor": actor, "runbook": rbID}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: run %s %s window %s", id, rbID, window), nil
	})
	if err != nil {
		return store.Work{}, store.Run{}, err
	}
	dispatchWebhook(projDir, w.ID, "work", "start", "", string(w.State), w.Title)
	return w, run, nil
}

// RecordRunStep records one step of the run: the owner's word (ok) or the exit
// that stopped it (pare), with UTC and BRT, flagged when an earlier step has no
// ok yet. A done or abandoned run refuses.
func RecordRunStep(repoDir, projDir, runID string, in RunStepInput, now time.Time) (store.RunEntry, error) {
	in.Step = strings.TrimSpace(in.Step)
	in.By = strings.TrimSpace(in.By)
	if in.Pare {
		in.Output = strings.TrimSpace(in.Output)
		if in.Output == "" {
			return store.RunEntry{}, fmt.Errorf("--pare needs the exit that stopped the run")
		}
	} else if strings.TrimSpace(in.Word) == "" {
		return store.RunEntry{}, fmt.Errorf("--ok needs the owner's literal word, quoted")
	}
	for flag, v := range map[string]string{"--por": in.By, "--ok": in.Word, "--pare": in.Output, "--esperada": in.Expected} {
		if err := refusePersonal(flag, v); err != nil {
			return store.RunEntry{}, err
		}
	}
	if strings.Contains(in.By, "@") {
		return store.RunEntry{}, fmt.Errorf("--por takes a nickname, not an e-mail or id")
	}
	var ev *store.RunEvidence
	if strings.TrimSpace(in.Evidence) != "" {
		e, err := HashEvidence(in.Evidence)
		if err != nil {
			return store.RunEntry{}, err
		}
		ev = &e
	}
	var entry store.RunEntry
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		w, err := store.LoadWork(repoDir, runID)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("run %s not found", runID)
		}
		if err != nil {
			return "", err
		}
		run, err := store.LoadRun(repoDir, runID)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("%s is not a runbook run (no execution record)", runID)
		}
		if err != nil {
			return "", err
		}
		if w.State != domain.WorkActive {
			return "", fmt.Errorf("run %s is %s (%s): it no longer takes steps", runID, store.RunClosed, w.State)
		}
		if run.Status() == store.RunStopped {
			return "", fmt.Errorf("run %s is %s (PARE at step %s): it no longer takes steps; open a new run", runID, store.RunStopped, run.Entries[len(run.Entries)-1].Step)
		}
		idx := slices.Index(run.Steps, in.Step)
		if idx < 0 {
			return "", fmt.Errorf("step %q is not in the run (steps: %s)", in.Step, strings.Join(run.Steps, " "))
		}
		out := false
		for _, prev := range run.Steps[:idx] {
			if !run.Explicit {
				break
			}
			if !runStepOK(run, prev) {
				out = true
				break
			}
		}
		state := RunOK
		if in.Pare {
			state = RunPare
		}
		exp := strings.TrimSpace(in.Expected)
		if exp == "" {
			exp = run.Expected
		}
		entry = store.RunEntry{
			Seq: len(run.Entries) + 1, Step: in.Step, State: state, By: in.By, OutOfOrder: out, Evidence: ev,
			UTC: now.UTC().Format(time.RFC3339), BRT: now.In(brt).Format("2006-01-02T15:04:05-07:00"),
		}
		if in.Pare {
			entry.Output = in.Output
		} else {
			entry.Word = strings.TrimSpace(in.Word)
			entry.Expected = exp
		}
		run.Entries = append(run.Entries, entry)
		if in.Pare {
			run.State = store.RunStopped
		}
		if err := store.SaveRun(repoDir, run); err != nil {
			return "", err
		}
		w.UpdatedAt = now.UTC()
		if err := store.SaveWork(repoDir, w); err != nil {
			return "", err
		}
		e := domain.NewEvent(runID, "run.step", map[string]string{"step": in.Step, "state": state}, EventActor())
		if err := store.AppendEvent(repoDir, e); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: run %s step %s %s", runID, in.Step, state), nil
	})
	return entry, err
}

func runStepOK(r store.Run, step string) bool {
	for _, e := range r.Entries {
		if e.Step == step && e.State == RunOK {
			return true
		}
	}
	return false
}

// RunDeviation is one recorded entry that departs from the runbook.
type RunDeviation struct {
	Entry   store.RunEntry
	Reasons []string
}

func normWord(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// RunDeviations lists the entries with a PARE, out of order, or with a word
// other than the expected one.
func RunDeviations(r store.Run) []RunDeviation {
	var out []RunDeviation
	for _, e := range r.Entries {
		var why []string
		if e.State == RunPare {
			why = append(why, "PARE")
		}
		if e.OutOfOrder {
			why = append(why, "out of order")
		}
		if e.State == RunOK && e.Expected != "" && normWord(e.Word) != normWord(e.Expected) {
			why = append(why, fmt.Sprintf("word differs from the expected %q", e.Expected))
		}
		if len(why) > 0 {
			out = append(out, RunDeviation{e, why})
		}
	}
	return out
}

// DraftRunErrata writes each deviation not yet reported as an erratum draft on
// the run's runbook (a proposal, with `Altera: <step>`, F-0872 item 1) and
// marks the entry with the item id, so a second report adds nothing new.
// Returns the item ids written, by entry sequence.
func DraftRunErrata(repoDir, projDir, runID, author string) ([]string, error) {
	var written []string
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		run, err := store.LoadRun(repoDir, runID)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("%s is not a runbook run (no execution record)", runID)
		}
		if err != nil {
			return "", err
		}
		for _, d := range RunDeviations(run) {
			if d.Entry.Drafted != "" {
				continue
			}
			text := fmt.Sprintf("Altera: %s\n\nRun %s (window %s), step %s at %s UTC / %s BRT: %s.",
				d.Entry.Step, run.ID, run.Window, d.Entry.Step, d.Entry.UTC, d.Entry.BRT, strings.Join(d.Reasons, "; "))
			if d.Entry.State == RunPare {
				text += fmt.Sprintf("\n\nExit reported: %s", d.Entry.Output)
			} else if d.Entry.Word != "" {
				text += fmt.Sprintf("\n\nWord recorded: %q", d.Entry.Word)
			}
			if d.Entry.Evidence != nil {
				text += fmt.Sprintf("\n\nEvidence: %s (sha256 %s)", d.Entry.Evidence.Path, d.Entry.Evidence.SHA256)
			}
			if err := store.AppendErrata(repoDir, run.Runbook, text, author, time.Now().UTC()); err != nil {
				return "", err
			}
			doc, err := store.LoadErrata(repoDir, run.Runbook)
			if err != nil {
				return "", err
			}
			items := store.ParseErrataItems(doc)
			id := items[len(items)-1].ID
			ev := domain.NewEvent(run.Runbook, "errata.added", map[string]string{"excerpt": errataExcerpt(strings.TrimPrefix(text, "Altera: "+d.Entry.Step+"\n\n"))}, author)
			if err := store.AppendEvent(repoDir, ev); err != nil {
				return "", err
			}
			for i := range run.Entries {
				if run.Entries[i].Seq == d.Entry.Seq {
					run.Entries[i].Drafted = id
				}
			}
			written = append(written, id)
		}
		if len(written) == 0 {
			return "", nil
		}
		if err := store.SaveRun(repoDir, run); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: errata draft from run %s", runID), nil
	})
	if errors.Is(err, gitback.ErrNothingToCommit) {
		return nil, nil
	}
	return written, err
}
