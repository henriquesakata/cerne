package lint

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// SpecChangedAfterApproval reports whether approved feature f's live spec
// differs from the copy its last approve kept (F-0318) — any byte counts — and
// returns both texts so a caller can show the delta. A spec edited back to the
// approved text is not changed, whatever edit events the log holds (the F-0262
// trail is still written; it no longer decides). Only an approved feature can
// be stale, and one with no copy — approved before F-0318 — never is: a
// missing (or unreadable) baseline is not a divergence. A re-approval, which
// renews the copy, is what clears it.
//
// Exported because the lint rule, the server payloads, show/next and the start
// gate all derive this one fact and must not disagree.
func SpecChangedAfterApproval(repoDir string, f store.Feature) (changed bool, approved, current []byte) {
	if f.State != domain.StateApproved {
		return false, nil, nil
	}
	approved, err := os.ReadFile(store.ApprovedSpecPath(repoDir, f.ID))
	if err != nil {
		return false, nil, nil
	}
	current, _ = os.ReadFile(filepath.Join(repoDir, "content", "features", f.ID, "spec.md"))
	return !bytes.Equal(approved, current), approved, current
}

// specChangedFindings flags the debt for features that are approved *right now*.
// Past approved the spec is no longer the live contract: in_development onward
// is covered by errata (F-0072) and drift (F-0113/F-0115).
func specChangedFindings(repoDir string, features []store.Feature) []Finding {
	var out []Finding
	for _, f := range features {
		if changed, _, _ := SpecChangedAfterApproval(repoDir, f); !changed {
			continue
		}
		out = append(out, Finding{
			Rule:   RuleSpecChangedAfterApproval,
			Entity: f.ID,
			File:   "content/features/" + f.ID + "/spec.md",
			Detail: fmt.Sprintf("spec editada depois da aprovação (%s) — o start fica bloqueado até um humano reaprovar com 'cerne feature approve %s', ou devolva ao backlog com 'cerne feature revert %s'", f.Title, f.ID, f.ID),
		})
	}
	return out
}
