package materialize

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// readDoc reads a materialized file under docsDir by its slash-rel path.
func readDoc(t *testing.T, docsDir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(docsDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// TestMaterializeOKFSingleFileType pins C1/C2/C4/C5/C6 for single-file
// entities: under okf=true each concept gains an OKF frontmatter block with the
// right `type`, the reserved/extra fields drawn from the entity, and the
// authored cerne_id preserved (not duplicated).
func TestMaterializeOKFSingleFileType(t *testing.T) {
	backend := seedBackend(t)
	addDecision(t, backend, "D-0001", "Usar Postgres", domain.DecisionActive,
		"---\ncerne_id: D-0001\n---\n\n# Usar Postgres\n\nDecisão.\n")
	addNote(t, backend, "N-0001", "go-git ignora ssh config", domain.NoteActive,
		"---\ncerne_id: N-0001\n---\n\nUse key-file fallback.\n")
	// Reference with URL, external marker and an informs backlink (the extras).
	r := store.Reference{ID: "R-0001", Title: "OKF spec", State: domain.ReferenceCurrent,
		URL: "https://cloud.google.com/okf", External: true, Informs: []string{"F-0054"},
		CreatedAt: seedTime, UpdatedAt: seedTime}
	if err := store.SaveReference(backend, r); err != nil {
		t.Fatalf("SaveReference: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backend, "content", "references", "R-0001.md"),
		[]byte("---\ncerne_id: R-0001\n---\n\n# OKF\n\nResumo.\n"), 0o644); err != nil {
		t.Fatalf("write reference body: %v", err)
	}

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", true); err != nil {
		t.Fatalf("Materialize okf: %v", err)
	}

	dec := readDoc(t, docsDir, "current/decisions/D-0001-usar-postgres.md")
	if !strings.HasPrefix(dec, "---\ntype: decision\n") {
		t.Errorf("decision must open with OKF frontmatter:\n%s", dec)
	}
	for _, want := range []string{
		"type: decision",
		"title: Usar Postgres",
		`timestamp: "2026-01-02T03:04:05Z"`, // RFC3339 has ":", so it is quoted
		"cerne_id: D-0001",
		"# Usar Postgres", // authored body survives the merge
	} {
		if !strings.Contains(dec, want) {
			t.Errorf("decision frontmatter/body missing %q:\n%s", want, dec)
		}
	}
	if strings.Count(dec, "cerne_id:") != 1 {
		t.Errorf("cerne_id duplicated after merge:\n%s", dec)
	}

	ref := readDoc(t, docsDir, "current/references/R-0001-okf-spec.md")
	for _, want := range []string{
		"type: reference",
		`resource: "https://cloud.google.com/okf"`,
		"external: true",
		"informs: [F-0054]",
	} {
		if !strings.Contains(ref, want) {
			t.Errorf("reference frontmatter missing %q:\n%s", want, ref)
		}
	}

	note := readDoc(t, docsDir, "current/notes/N-0001-go-git-ignora-ssh-config.md")
	if !strings.Contains(note, "type: note") {
		t.Errorf("note type missing:\n%s", note)
	}
}

// TestMaterializeOKFOffVerbatim pins C7: with okf=false the materialized body is
// the authored content byte-for-byte — no frontmatter injected, no behavior change.
func TestMaterializeOKFOffVerbatim(t *testing.T) {
	backend := seedBackend(t)
	const body = "---\ncerne_id: D-0001\n---\n\n# Usar Postgres\n\nDecisão.\n"
	addDecision(t, backend, "D-0001", "Usar Postgres", domain.DecisionActive, body)

	docsDir := filepath.Join(t.TempDir(), ".docs")
	manifest, err := Materialize(backend, docsDir, "", false)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got := readDoc(t, docsDir, "current/decisions/D-0001-usar-postgres.md"); got != body {
		t.Errorf("okf=false changed the body:\n--- got ---\n%s\n--- want ---\n%s", got, body)
	}
	// F-0166: the root OKF files are okf-only — never emitted when off.
	for _, reserved := range []string{"index.md", "log.md"} {
		if _, ok := manifest[reserved]; ok {
			t.Errorf("okf=false must not emit root %s", reserved)
		}
		if _, err := os.Stat(filepath.Join(docsDir, reserved)); !os.IsNotExist(err) {
			t.Errorf("okf=false wrote root %s (stat err = %v)", reserved, err)
		}
	}
}

// TestMaterializeOKFFeatureIndex pins C3: under okf=true a feature gains an
// index.md (type: feature) carrying its lifecycle fields, and each lifecycle doc
// gains its own type (spec|plan|result).
func TestMaterializeOKFFeatureIndex(t *testing.T) {
	backend := seedBackend(t, "finance")
	addFeature(t, backend, "F-0001", "Revamp de assinaturas", "finance", domain.StateInDevelopment, map[string]string{
		"spec.md": "---\ncerne_id: F-0001\n---\n\n# Spec\n\nConteúdo.\n",
		"plan.md": "---\ncerne_id: F-0001\n---\n\n# Plan\n\nConteúdo.\n",
	})
	addFeature(t, backend, "F-0002", "Tela de planos", "finance", domain.StateApproved, map[string]string{
		"spec.md": "---\ncerne_id: F-0002\n---\n\n# Spec\n\nConteúdo.\n",
	})
	// Make F-0002 a child so parent/epic surfaces in its index frontmatter.
	f2, err := store.LoadFeature(backend, "F-0002")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	f2.Parent = "F-0001"
	if err := store.SaveFeature(backend, f2); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}

	docsDir := filepath.Join(t.TempDir(), ".docs")
	manifest, err := Materialize(backend, docsDir, "", true)
	if err != nil {
		t.Fatalf("Materialize okf: %v", err)
	}

	idx := readDoc(t, docsDir, "current/features/F-0001-revamp-de-assinaturas/index.md")
	if !strings.HasPrefix(idx, "---\ntype: feature\n") {
		t.Errorf("index.md must open with type: feature:\n%s", idx)
	}
	for _, want := range []string{
		"title: Revamp de assinaturas",
		"state: in_development",
		"tags: [finance]",
		"cerne_id: F-0001",
		"- [spec](spec.md)",
		"- [plan](plan.md)",
	} {
		if !strings.Contains(idx, want) {
			t.Errorf("feature index missing %q:\n%s", want, idx)
		}
	}
	if _, ok := manifest["current/features/F-0001-revamp-de-assinaturas/index.md"]; !ok {
		t.Errorf("manifest missing feature index.md")
	}

	// Child feature index carries the parent (epic) link.
	childIdx := readDoc(t, docsDir, "current/features/F-0002-tela-de-planos/index.md")
	if !strings.Contains(childIdx, "parent: F-0001") {
		t.Errorf("child index missing parent:\n%s", childIdx)
	}

	// Each lifecycle doc carries its own type, merged over the authored cerne_id.
	spec := readDoc(t, docsDir, "current/features/F-0001-revamp-de-assinaturas/spec.md")
	if !strings.HasPrefix(spec, "---\ntype: spec\n") {
		t.Errorf("spec.md must open with type: spec:\n%s", spec)
	}
	if !strings.Contains(spec, "cerne_id: F-0001") {
		t.Errorf("spec.md lost cerne_id:\n%s", spec)
	}
	plan := readDoc(t, docsDir, "current/features/F-0001-revamp-de-assinaturas/plan.md")
	if !strings.Contains(plan, "type: plan") {
		t.Errorf("plan.md missing type: plan:\n%s", plan)
	}
}

// TestMaterializeOKFIdempotentAndValid pins C8 (the frontmatter emission is
// deterministic, so a re-pull does not churn the manifest), C9 (CONTEXT.md is
// unchanged by okf mode — the Docs column does not regress) and bundle validity
// (every materialized concept carries the required `type`).
func TestMaterializeOKFIdempotentAndValid(t *testing.T) {
	backend := seedBackend(t, "finance")
	addFeature(t, backend, "F-0001", "Parcelamento", "finance", domain.StateInDevelopment, map[string]string{
		"spec.md": "---\ncerne_id: F-0001\n---\n\n# Spec\n\nConteúdo.\n",
		"plan.md": "---\ncerne_id: F-0001\n---\n\n# Plan\n\nConteúdo.\n",
	})
	addDecision(t, backend, "D-0001", "Usar Postgres", domain.DecisionActive,
		"---\ncerne_id: D-0001\n---\n\n# Usar Postgres\n\nDecisão.\n")

	first := filepath.Join(t.TempDir(), ".docs")
	m1, err := Materialize(backend, first, "", true)
	if err != nil {
		t.Fatalf("first Materialize: %v", err)
	}
	second := filepath.Join(t.TempDir(), ".docs")
	m2, err := Materialize(backend, second, "", true)
	if err != nil {
		t.Fatalf("second Materialize: %v", err)
	}
	if !reflect.DeepEqual(m1, m2) {
		t.Fatalf("okf manifest is not deterministic:\n1: %v\n2: %v", m1, m2)
	}
	// F-0166: the root OKF files ship in the (deterministic) manifest.
	for _, reserved := range []string{"index.md", "log.md"} {
		if _, ok := m1[reserved]; !ok {
			t.Errorf("okf manifest missing root %s", reserved)
		}
	}

	// CONTEXT.md is identical with and without okf: the Docs column never regresses.
	off := filepath.Join(t.TempDir(), ".docs")
	mOff, err := Materialize(backend, off, "", false)
	if err != nil {
		t.Fatalf("off Materialize: %v", err)
	}
	if m1["CONTEXT.md"] != mOff["CONTEXT.md"] {
		t.Errorf("okf mode changed CONTEXT.md (Docs column regressed)")
	}

	// Bundle validity: every materialized concept .md opens with the required
	// OKF `type`. CONTEXT.md (cerne-native) and the root reserved filenames
	// (index.md, log.md — §5/§6/§7 of the OKF spec, not concepts) are exempt.
	err = filepath.WalkDir(first, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, rerr := filepath.Rel(first, p)
		if rerr != nil {
			return rerr
		}
		slash := filepath.ToSlash(rel)
		if d.IsDir() || !strings.HasSuffix(p, ".md") || d.Name() == "CONTEXT.md" || slash == "index.md" || slash == "log.md" {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if !strings.HasPrefix(string(data), "---\ntype:") {
			t.Errorf("concept %s lacks the required OKF type:\n%s", p, data)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
}

// TestMaterializeOKFDescriptionSingleFile pins C1 of F-0166: under okf=true a
// single-file concept with prose gains a one-line `description` — the first
// prose paragraph (headings, HTML comments and **metadata** lines skipped),
// collapsed and truncated at 200 runes — emitted right after `title`; a stub
// body yields no description at all.
func TestMaterializeOKFDescriptionSingleFile(t *testing.T) {
	backend := seedBackend(t)
	addDecision(t, backend, "D-0001", "Usar Postgres", domain.DecisionActive,
		"---\ncerne_id: D-0001\n---\n\n# Usar Postgres\n\n<!-- guia de template -->\n**Status:** ativo.\n\nAdotamos Postgres\ncomo banco padrão.\n\nSegundo parágrafo não entra.\n")
	long := strings.Repeat("á", 250)
	addNote(t, backend, "N-0001", "Nota longa", domain.NoteActive,
		"---\ncerne_id: N-0001\n---\n\n"+long+"\n")
	addReference(t, backend, "R-0001", "Stub ref", domain.ReferenceCurrent,
		"---\ncerne_id: R-0001\n---\n")

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", true); err != nil {
		t.Fatalf("Materialize okf: %v", err)
	}

	dec := readDoc(t, docsDir, "current/decisions/D-0001-usar-postgres.md")
	want := "title: Usar Postgres\ndescription: \"Adotamos Postgres como banco padrão.\"\n"
	if !strings.Contains(dec, want) {
		t.Errorf("decision must carry the collapsed first prose paragraph right after title:\nwant fragment %q in:\n%s", want, dec)
	}

	note := readDoc(t, docsDir, "current/notes/N-0001-nota-longa.md")
	wantNote := "description: \"" + strings.Repeat("á", 200) + "…\"\n"
	if !strings.Contains(note, wantNote) {
		t.Errorf("note description must truncate at 200 runes with …:\n%s", note)
	}

	ref := readDoc(t, docsDir, "current/references/R-0001-stub-ref.md")
	if strings.Contains(ref, "description:") {
		t.Errorf("stub body must not gain a description:\n%s", ref)
	}
}

// TestMaterializeOKFDescriptionFeatureIndex pins C2 of F-0166: the feature's
// index.md derives its description from the first prose paragraph of the
// spec's "## Problema" section, falling back to the body's first prose
// paragraph when the section is absent; a stub spec yields no description.
// Lifecycle docs themselves never gain a description (out of scope).
func TestMaterializeOKFDescriptionFeatureIndex(t *testing.T) {
	backend := seedBackend(t)
	addFeature(t, backend, "F-0001", "Revamp", "", domain.StateInDevelopment, map[string]string{
		"spec.md": "---\ncerne_id: F-0001\n---\n\n# Revamp\n\n**Status:** Rascunho.\n\n## Problema\n\nFalta X\nno sistema Y.\n\n## Objetivo\n\nOutro texto.\n",
	})
	addFeature(t, backend, "F-0002", "Planos", "", domain.StateApproved, map[string]string{
		"spec.md": "---\ncerne_id: F-0002\n---\n\nTexto direto do corpo.\n",
	})
	addFeature(t, backend, "F-0003", "Stub", "", domain.StateCandidate, map[string]string{
		"spec.md": "---\ncerne_id: F-0003\n---\n\n# Stub\n\n<!-- guia -->\n",
	})

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", true); err != nil {
		t.Fatalf("Materialize okf: %v", err)
	}

	idx := readDoc(t, docsDir, "current/features/F-0001-revamp/index.md")
	if !strings.Contains(idx, "description: Falta X no sistema Y.\n") {
		t.Errorf("index description must come from the Problema section:\n%s", idx)
	}
	spec := readDoc(t, docsDir, "current/features/F-0001-revamp/spec.md")
	if strings.Contains(spec, "description:") {
		t.Errorf("lifecycle docs must not gain a description:\n%s", spec)
	}

	fallback := readDoc(t, docsDir, "current/features/F-0002-planos/index.md")
	if !strings.Contains(fallback, "description: Texto direto do corpo.\n") {
		t.Errorf("index description must fall back to the body's first prose paragraph:\n%s", fallback)
	}

	stub := readDoc(t, docsDir, "current/features/F-0003-stub/index.md")
	if strings.Contains(stub, "description:") {
		t.Errorf("stub spec must not gain an index description:\n%s", stub)
	}
}

// TestMaterializeOKFLogMD pins C3 of F-0166: under okf=true the bundle root
// gains a log.md projected from the event log — entries grouped by UTC date,
// newest first, each as `* **<type>**: <entity> — <payload k=v> (<actor>)`,
// the entity id linked when (and only when) it is materialized in this pull,
// and the whole log capped at the 200 most recent entries.
func TestMaterializeOKFLogMD(t *testing.T) {
	backend := seedBackend(t)
	addDecision(t, backend, "D-0001", "Usar Postgres", domain.DecisionActive,
		"---\ncerne_id: D-0001\n---\n\n# Usar Postgres\n\nDecisão.\n")
	for _, ev := range []domain.Event{
		{ID: "ev-a", At: seedTime, EntityID: "D-0001", Type: "create",
			Payload: map[string]string{"title": "Usar Postgres"}, Actor: "henrique"},
		{ID: "ev-b", At: time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC), EntityID: "F-0099", Type: "transition",
			Payload: map[string]string{"from": "approved", "to": "in_development"}, Actor: "agent"},
	} {
		if err := store.AppendEvent(backend, ev); err != nil {
			t.Fatalf("AppendEvent %s: %v", ev.ID, err)
		}
	}

	docsDir := filepath.Join(t.TempDir(), ".docs")
	manifest, err := Materialize(backend, docsDir, "", true)
	if err != nil {
		t.Fatalf("Materialize okf: %v", err)
	}
	if _, ok := manifest["log.md"]; !ok {
		t.Fatalf("manifest missing log.md")
	}

	log := readDoc(t, docsDir, "log.md")
	for _, want := range []string{
		"* **transition**: F-0099 — from=approved, to=in_development (agent)",
		"* **create**: [D-0001](current/decisions/D-0001-usar-postgres.md) — title=Usar Postgres (henrique)",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log.md missing %q:\n%s", want, log)
		}
	}
	newer, older := strings.Index(log, "## 2026-01-03"), strings.Index(log, "## 2026-01-02")
	if newer < 0 || older < 0 || newer > older {
		t.Errorf("dates must group newest-first:\n%s", log)
	}

	// Cap: 205 events keep only the 200 most recent.
	capped := seedBackend(t)
	for i := 0; i < 205; i++ {
		ev := domain.Event{ID: fmt.Sprintf("ev-%03d", i), At: seedTime.Add(time.Duration(i) * time.Hour),
			EntityID: "X-0001", Type: "tick", Payload: map[string]string{"n": fmt.Sprintf("%03d", i)}}
		if err := store.AppendEvent(capped, ev); err != nil {
			t.Fatalf("AppendEvent %d: %v", i, err)
		}
	}
	cappedDocs := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(capped, cappedDocs, "", true); err != nil {
		t.Fatalf("Materialize capped: %v", err)
	}
	cl := readDoc(t, cappedDocs, "log.md")
	if got := strings.Count(cl, "* **"); got != 200 {
		t.Errorf("log entries = %d, want 200 (cap)", got)
	}
	if !strings.Contains(cl, "n=204") || strings.Contains(cl, "n=004") {
		t.Errorf("cap must keep the newest and drop the oldest entries:\n%.400s", cl)
	}
}

// TestMaterializeOKFRootIndex pins C4 of F-0166: under okf=true the bundle
// root gains an index.md whose frontmatter carries only okf_version, with one
// section per materialized kind, each concept as `* [title](path) — description`
// (the dash omitted when the concept has no description) and empty kinds
// omitted entirely.
func TestMaterializeOKFRootIndex(t *testing.T) {
	backend := seedBackend(t)
	addFeature(t, backend, "F-0001", "Revamp", "", domain.StateInDevelopment, map[string]string{
		"spec.md": "---\ncerne_id: F-0001\n---\n\n## Problema\n\nFalta X\nno sistema Y.\n",
	})
	addDecision(t, backend, "D-0001", "Usar Postgres", domain.DecisionActive,
		"---\ncerne_id: D-0001\n---\n\nAdotamos Postgres.\n")
	addReference(t, backend, "R-0001", "Stub ref", domain.ReferenceCurrent,
		"---\ncerne_id: R-0001\n---\n")

	docsDir := filepath.Join(t.TempDir(), ".docs")
	manifest, err := Materialize(backend, docsDir, "", true)
	if err != nil {
		t.Fatalf("Materialize okf: %v", err)
	}
	if _, ok := manifest["index.md"]; !ok {
		t.Fatalf("manifest missing index.md")
	}

	idx := readDoc(t, docsDir, "index.md")
	if !strings.HasPrefix(idx, "---\nokf_version: \"0.1\"\n---\n") {
		t.Errorf("root index frontmatter must carry only okf_version:\n%s", idx)
	}
	for _, want := range []string{
		"## Features",
		"* [Revamp](current/features/F-0001-revamp/index.md) — Falta X no sistema Y.",
		"## Decisions",
		"* [Usar Postgres](current/decisions/D-0001-usar-postgres.md) — Adotamos Postgres.",
		"## References",
		"* [Stub ref](current/references/R-0001-stub-ref.md)\n",
	} {
		if !strings.Contains(idx, want) {
			t.Errorf("root index missing %q:\n%s", want, idx)
		}
	}
	if strings.Contains(idx, "Stub ref](current/references/R-0001-stub-ref.md) —") {
		t.Errorf("concept without description must omit the dash:\n%s", idx)
	}
	if strings.Contains(idx, "## Runbooks") || strings.Contains(idx, "## Notes") {
		t.Errorf("empty kinds must be omitted:\n%s", idx)
	}
}

// TestMaterializeOKFAuthoredReservedNameWins pins C8 of F-0166: an authored
// feature doc named index.md wins over the generated OKF index — its content
// is materialized byte-for-byte and generation is skipped (OKF tolerates a
// missing index; overwriting authored content is never acceptable).
func TestMaterializeOKFAuthoredReservedNameWins(t *testing.T) {
	backend := seedBackend(t)
	const authored = "---\ncerne_id: F-0001\n---\n\n# Índice autorado\n"
	addFeature(t, backend, "F-0001", "Revamp", "", domain.StateInDevelopment, map[string]string{
		"spec.md":  "---\ncerne_id: F-0001\n---\n\nProsa da spec.\n",
		"index.md": authored,
	})

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", true); err != nil {
		t.Fatalf("Materialize okf: %v", err)
	}

	idx := readDoc(t, docsDir, "current/features/F-0001-revamp/index.md")
	if idx != authored {
		t.Errorf("authored index.md must be preserved byte-for-byte over the generated one:\n--- got ---\n%s\n--- want ---\n%s", idx, authored)
	}
}
