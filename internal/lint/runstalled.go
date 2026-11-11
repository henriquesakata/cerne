package lint

import (
	"errors"
	"fmt"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// runStalledFindings flags an active run (F-0873) that stopped on a PARE or has
// an ok on every step but was never closed: a state that would sit on the board
// as if the window were still going. Advisory.
func runStalledFindings(repoDir string, works []store.Work) ([]Finding, error) {
	var out []Finding
	for _, w := range works {
		if w.State != domain.WorkActive {
			continue
		}
		r, err := store.LoadRun(repoDir, w.ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if r.Status() == store.RunClosed {
			continue
		}
		why := ""
		if r.Status() == store.RunStopped {
			why = fmt.Sprintf("parada no passo %s (PARE)", r.Entries[len(r.Entries)-1].Step)
		} else if len(r.Steps) > 0 && allStepsOK(r) {
			why = "todos os passos com ok"
		}
		if why != "" {
			out = append(out, Finding{Rule: RuleRunStalled, Entity: w.ID,
				Detail: fmt.Sprintf("execução de %s (janela %s) %s e ainda ativa — 'cerne runbook report %s' e 'cerne work done %s' (ou 'cerne work abandon %s')", r.Runbook, r.Window, why, w.ID, w.ID, w.ID)})
		}
	}
	return out, nil
}

func allStepsOK(r store.Run) bool {
	ok := map[string]bool{}
	for _, e := range r.Entries {
		if e.State == "ok" {
			ok[e.Step] = true
		}
	}
	for _, s := range r.Steps {
		if !ok[s] {
			return false
		}
	}
	return true
}
