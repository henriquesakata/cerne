package server

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
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

// handleRoast (F-0271): explicit trigger only — runs the three fresh-context
// critic personas over the feature's current spec, persists the report as the
// roast.md artifact next to the candidate, and returns it. The roast never
// approves or rejects; the human gate consumes the findings.
func handleRoast(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
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

	cfg, _, err := project.LoadConfig(projDir)
	if err != nil || llm.ResolveMode(cfg.LLM.Mode) == llm.ModeDelegation {
		writeJSON(w, http.StatusOK, roastResponse{Generated: false, NeedsProvider: true})
		return
	}
	prov, err := llm.NewProvider(cfg.LLM)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}

	rep := app.RunRoast(r.Context(), prov, id, f.Title, string(specBytes), cfg.Agent.ChatTimeoutOrDefault())
	// A round where no critic finished is a cancelled request or a dead
	// provider, not a judgment — persisting it would overwrite a good report
	// with committed garbage.
	if rep.AllFailed() {
		writeJSON(w, http.StatusBadGateway, errorBody("roast: no critic finished ("+rep.Results[0].Err+")"))
		return
	}
	md := app.FormatRoastReport(rep)
	// One artifact per round (F-0277): the nanosecond stamp keeps names unique
	// and lexically sortable; earlier rounds survive as history.
	name := "roast-" + rep.At.Format("20060102T150405.000000000Z") + ".md"
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
}

// roastStampRe matches the stamp FormatRoastReport writes into every report:
// "Rodada: <RFC3339> · spec sha256:<8 hex>". Reading contract only — the
// writer lives in internal/app (F-0271).
var roastStampRe = regexp.MustCompile(`Rodada: (\S+) · spec sha256:([0-9a-f]{8})`)

// isRoastReport tells a persisted roast round apart from any other artifact
// sharing the dir: the bare legacy roast.md or a stamped roast-*.md. Markdown
// only — a user diagram slugged "roast-…" is not a report.
func isRoastReport(name string) bool {
	return name == "roast.md" ||
		(strings.HasPrefix(name, "roast-") && strings.HasSuffix(name, ".md"))
}

// roastRounds lists every persisted roast round for the detail payload,
// newest first (stampless rounds last), including the legacy single roast.md.
func roastRounds(repoDir, id, spec string) ([]roastRound, error) {
	dir := filepath.Join(repoDir, "content", "artifacts", id)
	names, err := filepath.Glob(filepath.Join(dir, "roast-*.md"))
	if err != nil {
		return nil, err
	}
	names = append(names, filepath.Join(dir, "roast.md"))
	curSHA := fmt.Sprintf("%x", sha256.Sum256([]byte(spec)))[:8]
	rounds := []roastRound{}
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			// A single unreadable round (swept by a concurrent pull, a stray
			// directory, permissions) must not 500 the whole feature page.
			continue
		}
		r := roastRound{Name: filepath.Base(name)}
		if m := roastStampRe.FindSubmatch(data); m != nil {
			if at, err := time.Parse(time.RFC3339, string(m[1])); err == nil {
				r.At = &at
			}
			r.SpecChanged = string(m[2]) != curSHA
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
