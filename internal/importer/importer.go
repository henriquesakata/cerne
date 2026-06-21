// Package importer onboards an existing documentation tree (Rockt Hub
// convention: case folders, ADRs, runbooks) into a cerne backend. Scan is a
// pure read-only pass producing candidates; Apply persists them.
package importer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/classify"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

const bom = "\xef\xbb\xbf"

// supersededImportReason is recorded on features imported in superseded
// state; the superseding feature is unknown at import time.
const supersededImportReason = "importado como substituído"

// featureDocNames is the copy order of a case folder's lifecycle documents.
var featureDocNames = []string{"spec.md", "plan.md", "result.md"}

// Candidate is one document (set) Scan mapped to a cerne entity.
type Candidate struct {
	Kind   string            // "feature" | "decision" | "reference"
	Title  string            // first "# " heading, fallback humanized folder/file name
	Module string            // <app> for apps/<app>/... cases, "" otherwise
	State  string            // mapped state (see mapping.go heuristics)
	Files  map[string]string // dest filename -> source abs path; decisions/references use {"doc.md": src}
	Source string            // rel slash path of the case dir (trailing "/") or file
}

// Summary aggregates one Apply run. ByState counts candidates by mapped
// state across all kinds (feature and decision "superseded" share a key).
type Summary struct {
	Features   int
	Decisions  int
	References int
	Runbooks   int
	ByState    map[string]int
}

// Scan walks rootDir for the Rockt Hub documentation convention and returns
// candidates in deterministic order: root cases by number, then per-app
// cases (app alphabetical, then number), then ADRs by number, then
// references alphabetical by path. Case folders without a spec.md are
// skipped entirely.
//
// modules are the project's declared modules: root cases (no path module) get a
// module inferred from their folder name and spec tags when exactly one matches
// (classify.InferModule); apps/<app> cases keep their path module regardless.
func Scan(rootDir string, modules []string) ([]Candidate, error) {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	cands, err := scanCases(root, "docs/process/cases", "", modules)
	if err != nil {
		return nil, err
	}
	apps, err := readDirNames(filepath.Join(root, "apps"), true)
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		appCands, err := scanCases(root, "apps/"+app+"/docs/process/cases", app, modules)
		if err != nil {
			return nil, err
		}
		cands = append(cands, appCands...)
	}
	decisions, err := scanDecisions(root)
	if err != nil {
		return nil, err
	}
	cands = append(cands, decisions...)
	references, err := scanReferences(root)
	if err != nil {
		return nil, err
	}
	cands = append(cands, references...)
	runbooks, err := scanRunbooks(root)
	if err != nil {
		return nil, err
	}
	return append(cands, runbooks...), nil
}

// scanCases maps every NNNN_name folder under <root>/<relCases> that has a
// spec.md to a feature candidate with the given module. When module is empty
// (root cases) it infers one from the folder name and spec tags via
// classify.InferModule against the declared modules; a non-empty module (an
// apps/<app> case) is always preserved as-is.
func scanCases(root, relCases, module string, modules []string) ([]Candidate, error) {
	dir := filepath.Join(root, filepath.FromSlash(relCases))
	names, err := readDirNames(dir, true)
	if err != nil {
		return nil, err
	}
	type caseDir struct {
		num       int
		name      string
		shortName string // kebab part after NNNN_, for the title fallback
	}
	var caseDirs []caseDir
	for _, name := range names {
		m := reCaseDir.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		num, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		caseDirs = append(caseDirs, caseDir{num: num, name: name, shortName: m[2]})
	}
	sort.Slice(caseDirs, func(i, j int) bool {
		if caseDirs[i].num != caseDirs[j].num {
			return caseDirs[i].num < caseDirs[j].num
		}
		return caseDirs[i].name < caseDirs[j].name
	})

	var cands []Candidate
	for _, cd := range caseDirs {
		caseAbs := filepath.Join(dir, cd.name)
		specBody, err := os.ReadFile(filepath.Join(caseAbs, "spec.md"))
		if errors.Is(err, os.ErrNotExist) {
			continue // folder without spec.md is skipped entirely
		}
		if err != nil {
			return nil, err
		}
		files := map[string]string{"spec.md": filepath.Join(caseAbs, "spec.md")}
		for _, name := range featureDocNames[1:] {
			p := filepath.Join(caseAbs, name)
			if _, err := os.Stat(p); err == nil {
				files[name] = p
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
		_, hasPlan := files["plan.md"]
		_, hasResult := files["result.md"]
		title := firstHeading(specBody)
		if title == "" {
			title = humanize(cd.shortName)
		}
		source := relCases + "/" + cd.name + "/"
		caseModule := module
		if caseModule == "" {
			if inferred, ok := classify.InferModule(source, string(specBody), modules); ok {
				caseModule = inferred
			}
		}
		cands = append(cands, Candidate{
			Kind:   "feature",
			Title:  title,
			Module: caseModule,
			State:  featureState(specBody, hasPlan, hasResult),
			Files:  files,
			Source: source,
		})
	}
	return cands, nil
}

// scanDecisions maps docs/architecture/adr/adr-NNN-*.md files to decision
// candidates ordered by ADR number.
func scanDecisions(root string) ([]Candidate, error) {
	relDir := "docs/architecture/adr"
	dir := filepath.Join(root, filepath.FromSlash(relDir))
	names, err := readDirNames(dir, false)
	if err != nil {
		return nil, err
	}
	type adrFile struct {
		num       int
		name      string
		shortName string // kebab title between adr-NNN- and .md, for the title fallback
	}
	var adrs []adrFile
	for _, name := range names {
		m := reADRFile.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		num, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		adrs = append(adrs, adrFile{num: num, name: name, shortName: m[2]})
	}
	sort.Slice(adrs, func(i, j int) bool {
		if adrs[i].num != adrs[j].num {
			return adrs[i].num < adrs[j].num
		}
		return adrs[i].name < adrs[j].name
	})

	var cands []Candidate
	for _, adr := range adrs {
		abs := filepath.Join(dir, adr.name)
		body, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		title := firstHeading(body)
		if title == "" {
			title = humanize(adr.shortName)
		}
		cands = append(cands, Candidate{
			Kind:   "decision",
			Title:  title,
			State:  decisionState(body),
			Files:  map[string]string{"doc.md": abs},
			Source: relDir + "/" + adr.name,
		})
	}
	return cands, nil
}

// referenceDirs are the documentation folders mapped to reference candidates.
// Each is listed non-recursively, so nested folders (e.g. architecture/adr,
// which becomes decisions) are excluded. docs/process (cases → features) and
// top-level docs/*.md (e.g. the AI-facing CLAUDE.md) are intentionally absent.
var referenceDirs = []string{
	"docs/architecture",
	"docs/compliance",
	"docs/design-system",
	"docs/finance",
	"docs/identity",
	"docs/planning",
	"docs/plans",
	"docs/plugins",
	"docs/review",
}

// scanReferences maps the *.md files directly under each referenceDirs entry
// to reference candidates, alphabetical by rel path.
func scanReferences(root string) ([]Candidate, error) {
	var sources []string
	for _, relDir := range referenceDirs {
		names, err := readDirNames(filepath.Join(root, filepath.FromSlash(relDir)), false)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if strings.HasSuffix(name, ".md") {
				sources = append(sources, relDir+"/"+name)
			}
		}
	}
	sort.Strings(sources)

	var cands []Candidate
	for _, source := range sources {
		abs := filepath.Join(root, filepath.FromSlash(source))
		body, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		title := firstHeading(body)
		if title == "" {
			title = humanize(strings.TrimSuffix(filepath.Base(abs), ".md"))
		}
		cands = append(cands, Candidate{
			Kind:   "reference",
			Title:  title,
			State:  "current",
			Files:  map[string]string{"doc.md": abs},
			Source: source,
		})
	}
	return cands, nil
}

// scanRunbooks maps docs/runbooks/*.md files to runbook candidates, alphabetical
// by filename. Runbooks are operational procedures; they are born active. (They
// were previously imported as references — see referenceDirs.)
func scanRunbooks(root string) ([]Candidate, error) {
	relDir := "docs/runbooks"
	names, err := readDirNames(filepath.Join(root, filepath.FromSlash(relDir)), false)
	if err != nil {
		return nil, err
	}
	var sources []string
	for _, name := range names {
		if strings.HasSuffix(name, ".md") {
			sources = append(sources, name)
		}
	}
	sort.Strings(sources)

	var cands []Candidate
	for _, name := range sources {
		abs := filepath.Join(root, filepath.FromSlash(relDir), name)
		body, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		title := firstHeading(body)
		if title == "" {
			title = humanize(strings.TrimSuffix(name, ".md"))
		}
		cands = append(cands, Candidate{
			Kind:   "runbook",
			Title:  title,
			State:  "active",
			Files:  map[string]string{"doc.md": abs},
			Source: relDir + "/" + name,
		})
	}
	return cands, nil
}

// readDirNames lists entry names in dir (dirs only when dirsOnly), sorted;
// nil when dir does not exist.
func readDirNames(dir string, dirsOnly bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if dirsOnly && !e.IsDir() {
			continue
		}
		if !dirsOnly && e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}

// ApplyOptions tunes Apply's link rewriting. SrcRoot is the source tree root,
// used to verify that source-code references still exist; BlobBase is the
// blob-URL prefix those references are permalinked to. Both empty leaves
// source-code references untouched.
type ApplyOptions struct {
	SrcRoot  string
	BlobBase string
}

// Apply persists the candidates into the backend at repoDir: sequential IDs
// per kind (continuing any existing sequence), entity JSON, one
// <kind>.imported event each, and content files with cerne_id/imported_from
// frontmatter whose cross-references are rewritten to resolve in the backend
// (see relink.go). IDs are assigned in a first pass so links between freshly
// imported documents resolve regardless of import order. No git interaction
// happens here.
func Apply(repoDir string, cands []Candidate, actor string, opts ApplyOptions) (Summary, error) {
	sum := Summary{ByState: map[string]int{}}
	now := time.Now().UTC()
	idx := NewLinkIndex()
	type placement struct {
		cand Candidate
		id   string
		docs []docTarget
	}
	var places []placement
	for _, c := range cands {
		id, docs, err := assignEntity(repoDir, c, now)
		if err != nil {
			return Summary{}, err
		}
		switch c.Kind {
		case "feature":
			sum.Features++
		case "decision":
			sum.Decisions++
		case "reference":
			sum.References++
		case "runbook":
			sum.Runbooks++
		}
		ev := domain.NewEvent(id, c.Kind+".imported", map[string]string{"source": c.Source}, actor)
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return Summary{}, err
		}
		sum.ByState[c.State]++
		idx.Add(c, id)
		places = append(places, placement{c, id, docs})
	}
	pb := PermalinkBase{BlobBase: opts.BlobBase, SrcRoot: opts.SrcRoot}
	for _, p := range places {
		for _, d := range p.docs {
			if err := copyDoc(d.src, d.dst, p.id, p.cand.Source, d.contentRel, idx, pb); err != nil {
				return Summary{}, err
			}
		}
	}
	return sum, nil
}

// docTarget is one content document to write: source and destination absolute
// paths, plus the backend-relative slash path that anchors its relative links.
type docTarget struct {
	src, dst, contentRel string
}

// assignEntity allocates the next id for c's kind, persists the entity JSON and
// returns the content documents to copy. Document bodies are untouched here so
// Apply can copy them in a second pass with the full link index built.
func assignEntity(repoDir string, c Candidate, now time.Time) (string, []docTarget, error) {
	switch c.Kind {
	case "feature":
		id, err := store.NextID(repoDir, "F")
		if err != nil {
			return "", nil, err
		}
		f := store.Feature{ID: id, Title: c.Title, Module: c.Module, State: domain.FeatureState(c.State), CreatedAt: now, UpdatedAt: now}
		if f.State == domain.StateSuperseded {
			f.Reason = supersededImportReason
		}
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", nil, err
		}
		var docs []docTarget
		for _, name := range featureDocNames {
			src, ok := c.Files[name]
			if !ok {
				continue
			}
			docs = append(docs, docTarget{
				src:        src,
				dst:        filepath.Join(repoDir, "content", "features", id, name),
				contentRel: "content/features/" + id + "/" + name,
			})
		}
		return id, docs, nil
	case "decision":
		id, err := store.NextID(repoDir, "D")
		if err != nil {
			return "", nil, err
		}
		d := store.Decision{ID: id, Title: c.Title, State: domain.DecisionState(c.State), CreatedAt: now, UpdatedAt: now}
		if err := store.SaveDecision(repoDir, d); err != nil {
			return "", nil, err
		}
		return id, []docTarget{{
			src:        c.Files["doc.md"],
			dst:        filepath.Join(repoDir, "content", "decisions", id+".md"),
			contentRel: "content/decisions/" + id + ".md",
		}}, nil
	case "reference":
		id, err := store.NextID(repoDir, "R")
		if err != nil {
			return "", nil, err
		}
		r := store.Reference{ID: id, Title: c.Title, State: domain.ReferenceState(c.State), CreatedAt: now, UpdatedAt: now}
		if err := store.SaveReference(repoDir, r); err != nil {
			return "", nil, err
		}
		return id, []docTarget{{
			src:        c.Files["doc.md"],
			dst:        filepath.Join(repoDir, "content", "references", id+".md"),
			contentRel: "content/references/" + id + ".md",
		}}, nil
	case "runbook":
		id, err := store.NextID(repoDir, "RB")
		if err != nil {
			return "", nil, err
		}
		rb := store.Runbook{ID: id, Title: c.Title, State: domain.RunbookState(c.State), CreatedAt: now, UpdatedAt: now}
		if err := store.SaveRunbook(repoDir, rb); err != nil {
			return "", nil, err
		}
		return id, []docTarget{{
			src:        c.Files["doc.md"],
			dst:        filepath.Join(repoDir, "content", "runbooks", id+".md"),
			contentRel: "content/runbooks/" + id + ".md",
		}}, nil
	}
	return "", nil, fmt.Errorf("unknown candidate kind %q (source %s)", c.Kind, c.Source)
}

// copyDoc reads src, rewrites its cross-references for the backend, and writes
// it to dst with cerne_id/imported_from frontmatter injected.
func copyDoc(src, dst, id, source, contentRel string, idx *LinkIndex, pb PermalinkBase) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	body = rewriteLinks(body, source, contentRel, idx, pb)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, injectFrontmatter(body, id, source), 0o644)
}

// injectFrontmatter returns body with cerne_id and imported_from as the
// first keys of the leading frontmatter block, prepending a fresh block when
// body does not already start with a closed "---" block.
func injectFrontmatter(body []byte, id, source string) []byte {
	keys := "cerne_id: " + id + "\nimported_from: " + source + "\n"
	content := bytes.TrimPrefix(body, []byte(bom))
	prefix := body[:len(body)-len(content)] // preserved BOM, if any
	var buf bytes.Buffer
	buf.Write(prefix)
	if off, ok := frontmatterOpen(content); ok {
		buf.Write(content[:off])
		buf.WriteString(keys)
		buf.Write(content[off:])
	} else {
		buf.WriteString("---\n" + keys + "---\n\n")
		buf.Write(content)
	}
	return buf.Bytes()
}

// frontmatterOpen reports whether content starts with a closed frontmatter
// block, returning the offset right after the opening "---" line.
func frontmatterOpen(content []byte) (int, bool) {
	lines := strings.SplitAfter(string(content), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return 0, false
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			return len(lines[0]), true
		}
	}
	return 0, false
}
