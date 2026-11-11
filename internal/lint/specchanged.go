package lint

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// SpecChangedAfterApproval reports whether approved feature f's live spec
// differs in content from the copy its last approve kept (F-0318) — one comma
// counts, the wrapping does not (specContent) — and returns both texts,
// normalized, so a caller shows exactly the delta that was compared. A spec
// edited back to the approved text is not changed, whatever edit events the
// log holds (the F-0262 trail is still written; it no longer decides). Only an approved feature can
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
	approved, current = specContent(approved), specContent(current)
	return !bytes.Equal(approved, current), approved, current
}

// specContent is what the comparison reads of a spec: its body, not its
// wrapping. A BOM and CRs go, every leading "---"-delimited frontmatter block
// goes (cerne_id, the OKF header 'cerne push' writes in okf mode — its
// timestamp churns on every transition — and a block doubled by a re-fed
// file), and so do trailing whitespace on each line and blank lines at both
// ends. An unterminated block is body. Everything else counts.
func specContent(data []byte) []byte {
	s := strings.ReplaceAll(strings.TrimPrefix(string(data), "\ufeff"), "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	for {
		for len(lines) > 0 && lines[0] == "" {
			lines = lines[1:]
		}
		if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
			break
		}
		end := slices.IndexFunc(lines[1:], func(l string) bool { return strings.TrimSpace(l) == "---" })
		if end < 0 {
			break
		}
		lines = lines[end+2:]
	}
	return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n"))
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
			Detail: fmt.Sprintf("spec changed since its approval (%s) — start is blocked until a human re-approves it with 'cerne feature approve %s', or 'cerne feature revert %s' sends it back to the Backlog", f.Title, f.ID, f.ID),
		})
	}
	return out
}
