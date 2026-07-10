package materialize

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
	if _, err := Materialize(backend, docsDir, "", false); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got := readDoc(t, docsDir, "current/decisions/D-0001-usar-postgres.md"); got != body {
		t.Errorf("okf=false changed the body:\n--- got ---\n%s\n--- want ---\n%s", got, body)
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

	// CONTEXT.md is identical with and without okf: the Docs column never regresses.
	off := filepath.Join(t.TempDir(), ".docs")
	mOff, err := Materialize(backend, off, "", false)
	if err != nil {
		t.Fatalf("off Materialize: %v", err)
	}
	if m1["CONTEXT.md"] != mOff["CONTEXT.md"] {
		t.Errorf("okf mode changed CONTEXT.md (Docs column regressed)")
	}

	// Bundle validity: every materialized concept .md (CONTEXT.md aside) opens
	// with the required OKF `type`.
	err = filepath.WalkDir(first, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") || d.Name() == "CONTEXT.md" {
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
