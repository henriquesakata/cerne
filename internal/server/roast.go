package server

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// roastResponse mirrors generateDiagramResponse: delegation mode signals
// needs_provider instead of failing, so the UI can explain what to configure.
type roastResponse struct {
	Generated     bool   `json:"generated"`
	NeedsProvider bool   `json:"needs_provider"`
	Partial       bool   `json:"partial"`
	Report        string `json:"report,omitempty"`
}

// roastRequest carries the target document (F-0275). An absent or empty doc
// means "spec" — the F-0271 callers keep working unchanged — but the default
// is explicit here rather than implied, and an unrecognized value is a client
// error: a typo must never quietly judge the wrong document.
type roastRequest struct {
	Doc string `json:"doc"`
}

// handleRoast (F-0271/F-0275/F-0277): explicit trigger only — runs three
// fresh-context critic personas over the feature's spec, or over spec+plan
// when the target is the plan, persists the round as its own artifact next to
// the candidate, and returns it. The roast never approves or rejects; the
// human gate consumes the findings.
func handleRoast(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	var req roastRequest
	// An empty body is the spec roast, same as {} — the F-0271 contract.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
		return
	}
	if req.Doc == "" {
		req.Doc = "spec"
	}
	if req.Doc != "spec" && req.Doc != "plan" {
		writeJSON(w, http.StatusBadRequest, errorBody("roast: unknown doc target "+req.Doc+" (spec|plan)"))
		return
	}

	f, err := store.LoadFeature(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("feature not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	switch f.State {
	case domain.StateDone, domain.StateRejected, domain.StateSuperseded:
		writeJSON(w, http.StatusConflict, errorBody("feature is terminal — nothing to roast"))
		return
	}

	specBytes, err := os.ReadFile(app.FeatureDocPath(repoDir, id, "spec.md"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusConflict, errorBody("feature has no spec to roast"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}

	personas, intro, prefix := app.RoastPersonas, app.SpecRoastIntro, "roast"
	docs := []app.RoastDoc{{Label: "SPEC", Body: string(specBytes)}}
	format := app.FormatRoastReport
	planTarget := req.Doc == "plan"
	if planTarget {
		planBytes, err := os.ReadFile(app.FeatureDocPath(repoDir, id, "plan.md"))
		// A missing plan and an untouched scaffold are the same situation:
		// nothing to judge. Three model calls on boilerplate produce
		// boilerplate findings. Checked before the provider, so the answer
		// does not depend on which LLM mode the project runs.
		if err != nil || !doccheck.HasRealContent(planBytes) {
			writeJSON(w, http.StatusConflict, errorBody("feature has no real plan to roast"))
			return
		}
		personas, intro, prefix = app.PlanRoastPersonas, app.PlanRoastIntro, "plan-roast"
		docs = append(docs, app.RoastDoc{Label: "PLANO", Body: string(planBytes)})
		format = app.FormatPlanRoastReport
	}

	cfg, _, err := project.LoadConfig(projDir)
	// No project config at all is genuinely "no provider configured"; a
	// .cernerc that exists and fails to parse is a config error. Reporting
	// both as needs_provider sends the user to the wrong place.
	if err != nil && !errors.Is(err, project.ErrNoProject) {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	if llm.ResolveMode(cfg.LLM.Mode) == llm.ModeDelegation {
		writeJSON(w, http.StatusOK, roastResponse{Generated: false, NeedsProvider: true})
		return
	}
	perCritic := cfg.Agent.ChatTimeoutOrDefault()
	if planTarget {
		// Two documents double a critic's input (F-0275 criterion 3).
		perCritic = cfg.Agent.PlanRoastTimeoutOrDefault()
	}
	prov, err := llm.NewProvider(cfg.LLM)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}

	rep := app.RunRoastWith(r.Context(), prov, personas, id, f.Title, intro, docs, perCritic)
	// A round where no critic finished is a cancelled request or a dead
	// provider, not a judgment — persisting it would overwrite a good report
	// with committed garbage.
	if rep.AllFailed() {
		writeJSON(w, http.StatusBadGateway, errorBody("roast: no critic finished ("+rep.FirstErr()+")"))
		return
	}
	md := format(rep)
	// One artifact per round (F-0277): the nanosecond stamp keeps names unique
	// and lexically sortable; earlier rounds survive as history. The plan
	// roast follows the same model under its own prefix (F-0275) — one
	// convention for both targets, not a single overwritten file for one.
	name := prefix + "-" + rep.At.Format("20060102T150405.000000000Z") + ".md"
	if err := app.SaveArtifact(repoDir, projDir, id, name, []byte(md)); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, roastResponse{Generated: true, Partial: rep.Partial, Report: md})
}

// roastRound is one persisted roast report in the detail payload (F-0277):
// At comes from the report's own stamp (nil for a stampless legacy file) and
// SpecChanged flags a round whose stamped spec sha no longer matches the
// current spec — computed here so the UI never hashes.
type roastRound struct {
	Name        string     `json:"name"`
	At          *time.Time `json:"at"`
	SpecChanged bool       `json:"spec_changed"`
	// PlanChanged is the plan-roast counterpart (F-0275): a plan round
	// asserts plan-against-spec conformity, so EITHER document moving
	// invalidates its verdicts. Always false for a spec round, whose report
	// carries no plan fingerprint.
	PlanChanged bool `json:"plan_changed"`
}

// roastStampAtRe matches the timestamp of the stamp every report carries:
// "Rodada: <RFC3339> · <label> sha256:<8 hex>…". The fingerprints themselves
// are read per label by app.ParseRoastSHA, since a plan round stamps two
// documents. Reading contract only — the writer lives in internal/app.
var roastStampAtRe = regexp.MustCompile(`Rodada: (\S+)`)

// isRoastReport tells a persisted roast round apart from any other artifact
// sharing the dir: the bare legacy roast.md or a stamped roast-*.md. Markdown
// only — a user diagram slugged "roast-…" is not a report.
func isRoastReport(name string) bool {
	if !strings.HasSuffix(name, ".md") {
		return false
	}
	return name == "roast.md" ||
		strings.HasPrefix(name, "roast-") ||
		strings.HasPrefix(name, "plan-roast-")
}

// roastRounds lists every persisted spec-roast round for the detail payload,
// newest first (stampless rounds last), including the legacy single roast.md.
func roastRounds(repoDir, id, spec string) ([]roastRound, error) {
	return collectRounds(repoDir, id, "roast-*.md", "roast.md", spec, "")
}

// planRoastRounds is the plan-roast counterpart (F-0275): the same per-round
// history model under its own prefix, with staleness against BOTH documents —
// a plan round judges the plan against the spec, so a spec edit invalidates
// it just as a plan edit does. There is no legacy single file to fold in.
func planRoastRounds(repoDir, id, spec, plan string) ([]roastRound, error) {
	return collectRounds(repoDir, id, "plan-roast-*.md", "", spec, plan)
}

// collectRounds reads the rounds matching glob (plus an optional legacy
// filename), stamping each with its time and which of the judged documents
// moved since. An empty plan means the caller judges the spec only.
func collectRounds(repoDir, id, glob, legacy, spec, plan string) ([]roastRound, error) {
	dir := filepath.Join(repoDir, "content", "artifacts", id)
	names, err := filepath.Glob(filepath.Join(dir, glob))
	if err != nil {
		return nil, err
	}
	if legacy != "" {
		names = append(names, filepath.Join(dir, legacy))
	}
	specSHA := fmt.Sprintf("%x", sha256.Sum256([]byte(spec)))[:8]
	planSHA := fmt.Sprintf("%x", sha256.Sum256([]byte(plan)))[:8]
	rounds := []roastRound{}
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			// A single unreadable round (swept by a concurrent pull, a stray
			// directory, permissions) must not 500 the whole feature page.
			continue
		}
		r := roastRound{Name: filepath.Base(name)}
		if m := roastStampAtRe.FindSubmatch(data); m != nil {
			if at, err := time.Parse(time.RFC3339, string(m[1])); err == nil {
				r.At = &at
			}
		}
		// An absent fingerprint proves nothing — a legacy or stampless round
		// reads as unchanged rather than permanently stale.
		if got := app.ParseRoastSHA(string(data), "spec"); got != "" {
			r.SpecChanged = got != specSHA
		}
		if plan != "" {
			if got := app.ParseRoastSHA(string(data), "plano"); got != "" {
				r.PlanChanged = got != planSHA
			}
		}
		rounds = append(rounds, r)
	}
	sort.SliceStable(rounds, func(i, j int) bool {
		a, b := rounds[i].At, rounds[j].At
		switch {
		case a == nil && b == nil:
			return rounds[i].Name > rounds[j].Name
		case a == nil:
			return false
		case b == nil:
			return true
		case !a.Equal(*b):
			return a.After(*b)
		}
		return rounds[i].Name > rounds[j].Name
	})
	return rounds, nil
}
