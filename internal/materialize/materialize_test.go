package materialize

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// seedBackend creates a plain backend dir (no git) with project.json.
func seedBackend(t *testing.T, modules ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo", Modules: modules}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	return dir
}

var seedTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func addFeature(t *testing.T, backendDir, id, title, module string, state domain.FeatureState, docs map[string]string) {
	t.Helper()
	f := store.Feature{ID: id, Title: title, Module: module, State: state, CreatedAt: seedTime, UpdatedAt: seedTime}
	if err := store.SaveFeature(backendDir, f); err != nil {
		t.Fatalf("SaveFeature %s: %v", id, err)
	}
	for name, body := range docs {
		path := filepath.Join(backendDir, "content", "features", id, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s/%s: %v", id, name, err)
		}
	}
}

func addDecision(t *testing.T, backendDir, id, title string, state domain.DecisionState, body string) {
	t.Helper()
	d := store.Decision{ID: id, Title: title, State: state, CreatedAt: seedTime, UpdatedAt: seedTime}
	if err := store.SaveDecision(backendDir, d); err != nil {
		t.Fatalf("SaveDecision %s: %v", id, err)
	}
	path := filepath.Join(backendDir, "content", "decisions", id+".md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func addReference(t *testing.T, backendDir, id, title string, state domain.ReferenceState, body string) {
	t.Helper()
	r := store.Reference{ID: id, Title: title, State: state, CreatedAt: seedTime, UpdatedAt: seedTime}
	if err := store.SaveReference(backendDir, r); err != nil {
		t.Fatalf("SaveReference %s: %v", id, err)
	}
	path := filepath.Join(backendDir, "content", "references", id+".md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func readContext(t *testing.T, docsDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(docsDir, "CONTEXT.md"))
	if err != nil {
		t.Fatalf("read CONTEXT.md: %v", err)
	}
	return string(data)
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s exists, want absent", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("Stat %s: %v", path, err)
	}
}

func TestMaterializeStateProjection(t *testing.T) {
	backend := seedBackend(t, "finance", "core")
	specBody := "---\ncerne_id: F-0003\n---\n\n# Parcelamento\n\nDividir compras em parcelas.\n"
	addFeature(t, backend, "F-0001", "Login social", "core", domain.StateCandidate,
		map[string]string{"spec.md": "spec F-0001\n"})
	addFeature(t, backend, "F-0002", "Exportar CSV", "core", domain.StateApproved,
		map[string]string{"spec.md": "spec F-0002\n", "plan.md": "plan F-0002\n"})
	addFeature(t, backend, "F-0003", "Parcelamento", "finance", domain.StateInDevelopment,
		map[string]string{"spec.md": specBody, "plan.md": "plan F-0003\n"})
	addFeature(t, backend, "F-0004", "Tema escuro", "core", domain.StateImplemented,
		map[string]string{"spec.md": "spec F-0004\n", "plan.md": "plan F-0004\n", "result.md": "result F-0004\n"})
	addFeature(t, backend, "F-0005", "Upload FTP", "core", domain.StateRejected,
		map[string]string{"spec.md": "spec F-0005\n"})
	addFeature(t, backend, "F-0006", "Senha local", "core", domain.StateSuperseded,
		map[string]string{"spec.md": "spec F-0006\n"})
	addDecision(t, backend, "D-0001", "Usar Postgres", domain.DecisionActive, "decision D-0001\n")
	addDecision(t, backend, "D-0002", "Usar MySQL", domain.DecisionSuperseded, "decision D-0002\n")
	addReference(t, backend, "R-0001", "Mapa do dominio", domain.ReferenceCurrent, "reference R-0001\n")
	addReference(t, backend, "R-0002", "Arquitetura antiga", domain.ReferenceObsolete, "reference R-0002\n")

	docsDir := filepath.Join(t.TempDir(), ".docs")
	manifest, err := Materialize(backend, docsDir, "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	// Active-ish features materialized with their existing docs.
	for _, rel := range []string{
		"current/features/F-0001-login-social/spec.md",
		"current/features/F-0002-exportar-csv/spec.md",
		"current/features/F-0002-exportar-csv/plan.md",
		"current/features/F-0003-parcelamento/spec.md",
		"current/features/F-0003-parcelamento/plan.md",
		"current/decisions/D-0001-usar-postgres.md",
		"current/references/R-0001-mapa-do-dominio.md",
	} {
		if _, err := os.Stat(filepath.Join(docsDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected materialized file %s: %v", rel, err)
		}
		if _, ok := manifest[rel]; !ok {
			t.Errorf("manifest missing %s", rel)
		}
	}

	// implemented / rejected / superseded content never materialized.
	mustNotExist(t, filepath.Join(docsDir, "current", "features", "F-0004-tema-escuro"))
	mustNotExist(t, filepath.Join(docsDir, "current", "features", "F-0005-upload-ftp"))
	mustNotExist(t, filepath.Join(docsDir, "current", "features", "F-0006-senha-local"))
	mustNotExist(t, filepath.Join(docsDir, "current", "decisions", "D-0002-usar-mysql.md"))
	mustNotExist(t, filepath.Join(docsDir, "current", "references", "R-0002-arquitetura-antiga.md"))

	ctx := readContext(t, docsDir)

	// The key product guarantee: superseded and rejected absent everywhere.
	for _, banned := range []string{"F-0005", "Upload FTP", "F-0006", "Senha local", "D-0002", "Usar MySQL", "R-0002", "Arquitetura antiga"} {
		if strings.Contains(ctx, banned) {
			t.Errorf("CONTEXT.md leaks %q:\n%s", banned, ctx)
		}
	}

	// Group order: in_development, approved, candidate, implemented.
	for _, pair := range [][2]string{{"F-0003", "F-0002"}, {"F-0002", "F-0001"}, {"F-0001", "F-0004"}} {
		if strings.Index(ctx, pair[0]) >= strings.Index(ctx, pair[1]) {
			t.Errorf("CONTEXT.md order: %s should come before %s:\n%s", pair[0], pair[1], ctx)
		}
	}

	// Implemented row present, state shown, Docs column empty.
	if !strings.Contains(ctx, "| F-0004 | implemented |  | core | Tema escuro |  |") {
		t.Errorf("CONTEXT.md missing implemented row without docs:\n%s", ctx)
	}

	// Manifest hashes: verify one manually.
	wantSum := sha256.Sum256([]byte(specBody))
	if got := manifest["current/features/F-0003-parcelamento/spec.md"]; got != hex.EncodeToString(wantSum[:]) {
		t.Errorf("manifest sha256 = %s, want %s", got, hex.EncodeToString(wantSum[:]))
	}
	if _, ok := manifest["CONTEXT.md"]; !ok {
		t.Errorf("manifest missing CONTEXT.md")
	}
	if _, ok := manifest[".gitkeep"]; ok {
		t.Errorf("manifest must not include .gitkeep")
	}
	// Every manifest entry hashes an existing file with matching content.
	for rel, wantHex := range manifest {
		data, err := os.ReadFile(filepath.Join(docsDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("manifest entry %s unreadable: %v", rel, err)
			continue
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != wantHex {
			t.Errorf("manifest entry %s hash mismatch", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(docsDir, ".gitkeep")); err != nil {
		t.Errorf(".gitkeep missing after materialize: %v", err)
	}
}

func TestMaterializeScope(t *testing.T) {
	backend := seedBackend(t, "finance", "core")
	addFeature(t, backend, "F-0001", "Parcelamento", "finance", domain.StateInDevelopment,
		map[string]string{"spec.md": "spec F-0001\n"})
	addFeature(t, backend, "F-0002", "Tema escuro", "core", domain.StateCandidate,
		map[string]string{"spec.md": "spec F-0002\n"})
	addDecision(t, backend, "D-0001", "Usar Postgres", domain.DecisionActive, "decision D-0001\n")
	addReference(t, backend, "R-0001", "Mapa do dominio", domain.ReferenceCurrent, "reference R-0001\n")

	docsDir := filepath.Join(t.TempDir(), ".docs")
	manifest, err := Materialize(backend, docsDir, "finance")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if _, ok := manifest["current/features/F-0001-parcelamento/spec.md"]; !ok {
		t.Errorf("scoped feature missing from manifest: %v", manifest)
	}
	mustNotExist(t, filepath.Join(docsDir, "current", "features", "F-0002-tema-escuro"))

	ctx := readContext(t, docsDir)
	if strings.Contains(ctx, "F-0002") || strings.Contains(ctx, "Tema escuro") {
		t.Errorf("CONTEXT.md leaks out-of-scope feature:\n%s", ctx)
	}
	// Decisions and references are project-wide: always included.
	for _, want := range []string{"D-0001", "R-0001"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("CONTEXT.md missing project-wide entry %s:\n%s", want, ctx)
		}
	}
}

func TestMaterializeSlugCollision(t *testing.T) {
	backend := seedBackend(t)
	addFeature(t, backend, "F-0001", "Backup", "", domain.StateCandidate,
		map[string]string{"spec.md": "spec F-0001\n"})
	addFeature(t, backend, "F-0002", "Backup", "", domain.StateCandidate,
		map[string]string{"spec.md": "spec F-0002\n"})

	docsDir := filepath.Join(t.TempDir(), ".docs")
	manifest, err := Materialize(backend, docsDir, "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	for _, rel := range []string{
		"current/features/F-0001-backup/spec.md",
		"current/features/F-0002-backup/spec.md",
	} {
		if _, ok := manifest[rel]; !ok {
			t.Errorf("manifest missing %s (same title must not collide): %v", rel, manifest)
		}
	}
}

func TestMaterializeContextGolden(t *testing.T) {
	backend := seedBackend(t, "finance", "core")
	addFeature(t, backend, "F-0001", "Parcelamento", "finance", domain.StateInDevelopment,
		map[string]string{"spec.md": "spec F-0001\n", "plan.md": "plan F-0001\n"})
	addFeature(t, backend, "F-0002", "Tema escuro", "core", domain.StateImplemented,
		map[string]string{"spec.md": "spec F-0002\n"})
	addDecision(t, backend, "D-0001", "Usar Postgres", domain.DecisionActive, "decision D-0001\n")

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, ""); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	want := `# Demo — estado atual

> Gerado por cerne. Não edite: rode 'cerne pull' para atualizar.
> Histórico e documentos substituídos: 'cerne search'.

## Features

| ID | Estado | Épico | Módulo | Título | Docs |
|---|---|---|---|---|---|
| F-0001 | in_development |  | finance | Parcelamento | [spec](current/features/F-0001-parcelamento/spec.md), [plan](current/features/F-0001-parcelamento/plan.md) |
| F-0002 | implemented |  | core | Tema escuro |  |

## Épicos

_Nenhum._

## Decisões ativas

- [D-0001 — Usar Postgres](current/decisions/D-0001-usar-postgres.md)

## Runbooks

_Nenhum._

## Referências

_Nenhuma._
`
	if got := readContext(t, docsDir); got != want {
		t.Errorf("CONTEXT.md mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestMaterializeContextEpics(t *testing.T) {
	backend := seedBackend(t)
	save := func(f store.Feature) {
		t.Helper()
		f.CreatedAt, f.UpdatedAt = seedTime, seedTime
		if err := store.SaveFeature(backend, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	save(store.Feature{ID: "F-0001", Title: "Revamp de assinaturas", State: domain.StateInDevelopment})
	save(store.Feature{ID: "F-0002", Title: "Tela de planos", Parent: "F-0001", State: domain.StateImplemented})
	save(store.Feature{ID: "F-0003", Title: "Cobrança recorrente", Parent: "F-0001", State: domain.StateApproved})
	save(store.Feature{ID: "F-0004", Title: "Ideia descartada", Parent: "F-0001", State: domain.StateRejected})
	save(store.Feature{ID: "F-0005", Title: "Feature solta", State: domain.StateCandidate})

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, ""); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	ctx := readContext(t, docsDir)

	// Épico column carries the parent ID on a child row.
	if !strings.Contains(ctx, "| F-0003 | approved | F-0001 |  | Cobrança recorrente |  |") {
		t.Errorf("missing child row with Épico column:\n%s", ctx)
	}
	// Épicos breakdown: progress counts only live children (rejected excluded → 1/2).
	if !strings.Contains(ctx, "### F-0001 — Revamp de assinaturas (1/2 implementadas)") {
		t.Errorf("missing epic heading with progress:\n%s", ctx)
	}
	for _, want := range []string{
		"- F-0002 — Tela de planos (implemented)",
		"- F-0003 — Cobrança recorrente (approved)",
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("missing epic child %q:\n%s", want, ctx)
		}
	}
	if strings.Contains(ctx, "Ideia descartada") {
		t.Errorf("rejected child leaked into CONTEXT.md:\n%s", ctx)
	}
	if strings.Contains(ctx, "### F-0005") {
		t.Errorf("non-epic F-0005 rendered as an epic:\n%s", ctx)
	}
}

func TestMaterializeEmptyBackend(t *testing.T) {
	backend := seedBackend(t)
	docsDir := filepath.Join(t.TempDir(), ".docs")
	manifest, err := Materialize(backend, docsDir, "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	ctx := readContext(t, docsDir)
	for _, want := range []string{
		"## Features\n\n_Nenhuma._\n",
		"## Épicos\n\n_Nenhum._\n",
		"## Decisões ativas\n\n_Nenhuma._\n",
		"## Referências\n\n_Nenhuma._\n",
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("CONTEXT.md missing empty section %q:\n%s", want, ctx)
		}
	}
	if len(manifest) != 1 {
		t.Errorf("manifest = %v, want only CONTEXT.md", manifest)
	}
}

func TestMaterializeEntryWithoutContentDoc(t *testing.T) {
	backend := seedBackend(t)
	// Active decision whose content doc is missing in the backend: listed
	// as plain text (no dead link), nothing materialized for it.
	d := store.Decision{ID: "D-0001", Title: "Usar Postgres", State: domain.DecisionActive, CreatedAt: seedTime, UpdatedAt: seedTime}
	if err := store.SaveDecision(backend, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}

	docsDir := filepath.Join(t.TempDir(), ".docs")
	manifest, err := Materialize(backend, docsDir, "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	ctx := readContext(t, docsDir)
	if !strings.Contains(ctx, "- D-0001 — Usar Postgres\n") {
		t.Errorf("CONTEXT.md missing plain bullet for content-less decision:\n%s", ctx)
	}
	if strings.Contains(ctx, "](current/decisions/") {
		t.Errorf("CONTEXT.md links a decision with no materialized content:\n%s", ctx)
	}
	if _, ok := manifest["current/decisions/D-0001-usar-postgres.md"]; ok {
		t.Errorf("manifest tracks a file that was never written: %v", manifest)
	}
}

func TestMaterializeWipesSupersededOnRerun(t *testing.T) {
	backend := seedBackend(t)
	addFeature(t, backend, "F-0001", "Login social", "", domain.StateInDevelopment,
		map[string]string{"spec.md": "spec F-0001\n", "plan.md": "plan F-0001\n"})

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, ""); err != nil {
		t.Fatalf("first Materialize: %v", err)
	}
	featureDir := filepath.Join(docsDir, "current", "features", "F-0001-login-social")
	if _, err := os.Stat(filepath.Join(featureDir, "spec.md")); err != nil {
		t.Fatalf("spec.md after first run: %v", err)
	}

	// Feature transitions to superseded: re-running must remove its dir.
	f, err := store.LoadFeature(backend, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	f.State = domain.StateSuperseded
	if err := store.SaveFeature(backend, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}

	manifest, err := Materialize(backend, docsDir, "")
	if err != nil {
		t.Fatalf("second Materialize: %v", err)
	}
	mustNotExist(t, featureDir)
	if strings.Contains(readContext(t, docsDir), "F-0001") {
		t.Errorf("CONTEXT.md still mentions superseded F-0001")
	}
	for rel := range manifest {
		if strings.Contains(rel, "F-0001") {
			t.Errorf("manifest still tracks %s", rel)
		}
	}
}
