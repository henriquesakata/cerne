package app

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// mdLinkRx matches an inline markdown link [text](target).
var mdLinkRx = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)

// runbookSourcePrefix marks the imported_from of a doc that came from the hub's
// runbooks tree. References with this prefix were imported before the runbook
// entity existed (F-0040); the migration reclassifies them as runbooks.
const runbookSourcePrefix = "docs/runbooks/"

// RunbookMigration is one reference→runbook reclassification: FromRef is the old
// reference id, ToRunbook the minted runbook id (empty in a dry-run plan).
type RunbookMigration struct {
	FromRef   string
	ToRunbook string
	Title     string
	Source    string // imported_from of the content doc
}

// PlanRunbookMigration returns, in id order, the references that would be
// reclassified as runbooks — those whose content doc was imported_from
// docs/runbooks/. It writes nothing and mints no ids (dry-run).
func PlanRunbookMigration(repoDir string) ([]RunbookMigration, error) {
	refs, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return nil, err
	}
	var plan []RunbookMigration
	for _, r := range refs {
		src, err := referenceSource(repoDir, r.ID)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(src, runbookSourcePrefix) {
			continue
		}
		plan = append(plan, RunbookMigration{FromRef: r.ID, Title: r.Title, Source: src})
	}
	return plan, nil
}

// referenceSource reads the imported_from of a reference's content doc, or "".
func referenceSource(repoDir, id string) (string, error) {
	data, err := os.ReadFile(ReferenceDocPath(repoDir, id))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return doccheck.FrontmatterValue(data, "imported_from"), nil
}

// MigrateRunbooksFromReferences reclassifies every reference imported from
// docs/runbooks/ into a runbook: it mints an RB id, writes the runbook entity,
// moves the content doc (rewriting its cerne_id), removes the old reference and
// its doc, rewrites cross-links across content/ (references/<R>.md →
// runbooks/<RB>.md and the bare id <R> → <RB>), and appends a runbook.migrated
// event each. It does NOT commit — the caller commits+pushes. Returns the
// applied migrations, nil when there is nothing to migrate. Idempotent: a
// second run finds no runbook references and is a no-op.
func MigrateRunbooksFromReferences(repoDir string) ([]RunbookMigration, error) {
	plan, err := PlanRunbookMigration(repoDir)
	if err != nil {
		return nil, err
	}
	if len(plan) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	remap := make(map[string]string, len(plan))
	for i := range plan {
		id, err := store.NextID(repoDir, "RB")
		if err != nil {
			return nil, err
		}
		plan[i].ToRunbook = id
		remap[plan[i].FromRef] = id

		if err := store.SaveRunbook(repoDir, store.Runbook{
			ID:        id,
			Title:     plan[i].Title,
			State:     domain.RunbookActive,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return nil, err
		}
		if err := moveDocToRunbook(repoDir, plan[i].FromRef, id); err != nil {
			return nil, err
		}
		if err := store.DeleteReference(repoDir, plan[i].FromRef); err != nil {
			return nil, err
		}
		ev := domain.NewEvent(id, "runbook.migrated", map[string]string{
			"from":   plan[i].FromRef,
			"source": plan[i].Source,
		}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return nil, err
		}
	}
	if err := rewriteRunbookCrossLinks(repoDir, remap); err != nil {
		return nil, err
	}
	return plan, nil
}

// moveDocToRunbook moves content/references/<ref>.md to content/runbooks/<rb>.md,
// rewriting the cerne_id frontmatter from the old to the new id.
func moveDocToRunbook(repoDir, ref, rb string) error {
	src := ReferenceDocPath(repoDir, ref)
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	body := strings.Replace(string(data), "cerne_id: "+ref, "cerne_id: "+rb, 1)
	dst := RunbookDocPath(repoDir, rb)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

// rewriteRunbookCrossLinks rewrites, across every content/*.md doc, the links and
// mentions affected by the migration. It is link-aware: a markdown link is
// resolved relative to the doc's ORIGINAL directory (content/references for a doc
// that just moved into content/runbooks) and re-emitted relative to its CURRENT
// directory, retargeting migrated references to content/runbooks/<RB>.md. This
// fixes both a non-moved doc pointing at a migrated ref and a moved doc whose
// same-directory links would otherwise dangle after the move. A second pass
// rewrites the bare id <R> → <RB> for prose mentions and link text.
func rewriteRunbookCrossLinks(repoDir string, remap map[string]string) error {
	migrated := make(map[string]bool, len(remap)) // RB ids that just moved from references/
	for _, to := range remap {
		migrated[to] = true
	}
	type bareRule struct {
		rx *regexp.Regexp
		to string
	}
	bares := make([]bareRule, 0, len(remap))
	for from, to := range remap {
		bares = append(bares, bareRule{regexp.MustCompile(`\b` + regexp.QuoteMeta(from) + `\b`), to})
	}

	root := filepath.Join(repoDir, "content")
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil // no content tree yet — nothing to rewrite
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(repoDir, p)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		newDir := path.Dir(relSlash)
		// A doc now at content/runbooks/<RB>.md whose id was just minted moved
		// from content/references; its links were written relative to there.
		oldDir := newDir
		moved := false
		if base := strings.TrimSuffix(path.Base(relSlash), ".md"); strings.HasPrefix(relSlash, "content/runbooks/") && migrated[base] {
			oldDir = "content/references"
			moved = true
		}

		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out := mdLinkRx.ReplaceAllStringFunc(string(data), func(m string) string {
			sub := mdLinkRx.FindStringSubmatch(m)
			target, frag := splitFragment(strings.TrimSpace(sub[2]))
			cur, ok := currentLinkTarget(oldDir, target, remap, moved)
			if !ok {
				return m
			}
			return "[" + sub[1] + "](" + relLink(newDir, cur) + frag + ")"
		})
		for _, b := range bares {
			out = b.rx.ReplaceAllString(out, b.to)
		}
		if out == string(data) {
			return nil
		}
		return os.WriteFile(p, []byte(out), 0o644)
	})
}

// currentLinkTarget resolves a markdown link target (written relative to oldDir)
// to the CURRENT content-relative path of what it points at, or ok=false when the
// link should be left untouched. A link to a migrated reference retargets to
// content/runbooks/<RB>.md. For a moved doc, every internal .md link is returned
// so it can be re-relativized from the doc's new location; for a non-moved doc
// only migrated-reference links are rewritten (no churn on unrelated links).
func currentLinkTarget(oldDir, target string, remap map[string]string, moved bool) (string, bool) {
	if target == "" || !strings.HasSuffix(target, ".md") || isExternalLink(target) {
		return "", false
	}
	resolved := path.Clean(oldDir + "/" + target)
	if !strings.HasPrefix(resolved, "content/") {
		return "", false
	}
	if id := strings.TrimPrefix(resolved, "content/references/"); id != resolved {
		if rb, ok := remap[strings.TrimSuffix(id, ".md")]; ok {
			return "content/runbooks/" + rb + ".md", true
		}
	}
	if moved {
		return resolved, true
	}
	return "", false
}

// splitFragment splits a link target into its path and a trailing #fragment.
func splitFragment(s string) (base, frag string) {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

// isExternalLink reports whether target is a URL, mail, anchor, or absolute path
// (not an internal relative content link).
func isExternalLink(target string) bool {
	low := strings.ToLower(target)
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") ||
		strings.HasPrefix(low, "mailto:") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "/")
}

// relLink returns target (a content-relative slash path) relative to fromDir.
func relLink(fromDir, target string) string {
	r, err := filepath.Rel(filepath.FromSlash(fromDir), filepath.FromSlash(target))
	if err != nil {
		return target
	}
	return filepath.ToSlash(r)
}
