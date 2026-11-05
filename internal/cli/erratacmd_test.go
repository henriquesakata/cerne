package cli

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestErrataCreatesRecordOneShot(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	out, err := execCerne(t, "feature", "errata", "F-0001", "Correção: usar D-0002.", "--dir", projDir)
	if err != nil {
		t.Fatalf("errata: %v\n%s", err, out)
	}
	if !strings.Contains(out, "erratum appended to F-0001") {
		t.Fatalf("missing confirmation:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "errata", "F-0001.md"))
	if err != nil || !strings.Contains(string(data), "usar D-0002") {
		t.Fatalf("errata file missing/wrong (err %v):\n%s", err, data)
	}
}

func TestErrataFromFile(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	bodyFile := filepath.Join(t.TempDir(), "errata.txt")
	if err := os.WriteFile(bodyFile, []byte("Correção via arquivo."), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerne(t, "feature", "errata", "F-0001", "--file", bodyFile, "--dir", projDir); err != nil {
		t.Fatalf("errata --file: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(filepath.Join(repoDir, "content", "errata", "F-0001.md"))
	if !strings.Contains(string(data), "via arquivo") {
		t.Fatalf("errata --file body missing:\n%s", data)
	}
}

func TestErrataWiredOnEachEntity(t *testing.T) {
	root := Root()
	for _, parent := range []string{"feature", "decision", "runbook", "note"} {
		if !hasSubcommand(root, parent, "errata") {
			t.Errorf("%s errata subcommand not wired", parent)
		}
	}
}

func TestShowAppendsErrata(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "errata", "F-0001", "primeira nota", "--dir", projDir); err != nil {
		t.Fatalf("errata 1: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "errata", "F-0001", "segunda nota", "--dir", projDir); err != nil {
		t.Fatalf("errata 2: %v\n%s", err, out)
	}
	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "errata") || !strings.Contains(out, "primeira nota") || !strings.Contains(out, "segunda nota") {
		t.Fatalf("show missing errata entries:\n%s", out)
	}
	if strings.Index(out, "primeira nota") > strings.Index(out, "segunda nota") {
		t.Fatalf("errata entries out of chronological order:\n%s", out)
	}
}

func TestShowJSONIncludesErrata(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "errata", "F-0001", "nota json", "--dir", projDir); err != nil {
		t.Fatalf("errata: %v\n%s", err, out)
	}
	out, err := execCerne(t, "show", "F-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --json: %v\n%s", err, out)
	}
	if !strings.Contains(out, "errata") || !strings.Contains(out, "nota json") {
		t.Fatalf("show --json missing errata:\n%s", out)
	}
}

// TestNoCommandRewritesConcludedBody pins F-0072 crit. 5 at the surface level: no
// command rewrites the body of a concluded entity — errata is the correction
// path. F-0305 moved the boundary for features from implemented to done, by the
// product owner's decision recorded in the F-0305 spec: `feature amend` replaces
// the result of an implemented feature (before the accept), so the table below
// proves it refuses done, rejected and superseded with the file intact.
func TestNoCommandRewritesConcludedBody(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if n := sub.Name(); n == "edit" || n == "rewrite" {
				t.Errorf("found a body-rewriting subcommand %q under %q — corrections must be additive (errata) only", n, c.Name())
			}
			walk(sub)
		}
	}
	walk(Root())

	projDir, _, repoDir := setupProject(t)
	for state, id := range seedResultFeatures(t, repoDir, domain.StateDone, domain.StateRejected, domain.StateSuperseded) {
		t.Run(string(state), func(t *testing.T) {
			assertAmendRefused(t, projDir, repoDir, id, "cerne feature errata "+id)
		})
	}
}

// TestDraftCommandsRefuseClosedFeature: spec/plan/result draft are document
// writes too (F-0305) — past the live states each refuses with ErrFeatureFrozen
// and the state's path, even in delegation mode, so no prompt and no "write it
// and push" hint is printed, and the backend stays put. A live feature still
// gets the prompt.
func TestDraftCommandsRefuseClosedFeature(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	ids := seedResultFeatures(t, repoDir, domain.StateInDevelopment, domain.StateImplemented,
		domain.StateDone, domain.StateRejected, domain.StateSuperseded)
	head0, _ := gitback.HeadSHA(origin)
	for _, doc := range []string{"spec", "plan", "result"} {
		for state, id := range ids {
			out, err := execCerne(t, doc, "draft", id, "--dir", projDir)
			if state == domain.StateInDevelopment {
				if err != nil || !strings.Contains(out, "run 'cerne push'") {
					t.Fatalf("%s draft %s (%s): err = %v, want the prompt:\n%s", doc, id, state, err, out)
				}
				continue
			}
			want := wantFix(state, id, doc)
			if !errors.Is(err, app.ErrFeatureFrozen) || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s draft %s (%s): err = %v, want ErrFeatureFrozen naming %q", doc, id, state, err, want)
			}
			if strings.Contains(out, "- id: "+id) || strings.Contains(out, "run 'cerne push'") {
				t.Fatalf("%s draft %s (%s) printed the prompt:\n%s", doc, id, state, out)
			}
		}
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved: %s -> %s", head0, h)
	}
}

// TestDraftRechecksStateInsideTheLock: the provider call sits outside the
// backend lock, so a feature closed while the model was drafting is refused
// inside it — the drafted text never lands (F-0305).
func TestDraftRechecksStateInsideTheLock(t *testing.T) {
	for _, doc := range []string{"spec", "plan", "result"} {
		t.Run(doc, func(t *testing.T) {
			projDir, origin, repoDir := setupProject(t)
			id := seedResultFeatures(t, repoDir, domain.StateInDevelopment)[domain.StateInDevelopment]
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				f, _ := store.LoadFeature(repoDir, id)
				f.State = domain.StateImplemented // a `complete` landing mid-draft
				if err := store.SaveFeature(repoDir, f); err != nil {
					t.Error(err)
				}
				commitBackend(t, repoDir)
				_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"## Rascunho\n\n`+strings.Repeat("Texto que nunca deve chegar ao backend. ", 8)+`"}]}`)
			}))
			defer srv.Close()
			t.Setenv("TEST_BYO_KEY", "sk-byo")
			enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")
			_, err := execCerne(t, doc, "draft", id, "--dir", projDir)
			if !errors.Is(err, app.ErrFeatureFrozen) {
				t.Fatalf("err = %v, want ErrFeatureFrozen", err)
			}
			got, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", id, doc+".md"))
			if strings.Contains(string(got), "nunca deve chegar") {
				t.Fatalf("drafted text written:\n%s", got)
			}
			head, _ := gitback.HeadSHA(origin)
			if local, _ := gitback.HeadSHA(repoDir); local != head {
				t.Fatalf("clone %s ahead of origin %s", local, head)
			}
		})
	}
}

func hasSubcommand(root *cobra.Command, parent, child string) bool {
	for _, c := range root.Commands() {
		if c.Name() != parent {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() == child {
				return true
			}
		}
	}
	return false
}

// TestErrataClearsResultDebtEndToEnd: a feature that froze with a result in
// debt leaves the lint through `feature errata` alone (F-0305, crit. 5).
func TestErrataClearsResultDebtEndToEnd(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance")
	now := time.Now().UTC()
	f := store.Feature{ID: "F-0001", Title: "Boletos", State: domain.StateImplemented, Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", "---\ncerne_id: F-0001\n---\n\n# Boletos\n\n## Objetivo e critérios de aceitação\n\n1. a\n2. b\n")
	writeDoc(t, repoDir, "F-0001", "plan.md", realDoc("F-0001", "passos"))
	writeDoc(t, repoDir, "F-0001", "result.md", "---\ncerne_id: F-0001\n---\n\n# Boletos\n\ncritério 1 provado.\n")
	if err := store.AppendEvent(repoDir, domain.Event{EntityID: "F-0001", Type: "feature.completed", At: now}); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	resultPath := filepath.Join(repoDir, "content", "features", "F-0001", "result.md")
	before, _ := os.ReadFile(resultPath)

	out, _ := execCerne(t, "lint", "--dir", projDir)
	if !strings.Contains(out, "RESULT-MISSING-EVIDENCE") || !strings.Contains(out, "RESULT-MISSING-GAUNTLET") {
		t.Fatalf("want both findings before errata:\n%s", out)
	}
	errFile := filepath.Join(t.TempDir(), "errata.md")
	body := "critério 2 provado.\n\n## Gauntlet\n\nBarra: fixture de teste — referência nomeada\nRodada 1 — vitória: \"fixture\"\n"
	if err := os.WriteFile(errFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerne(t, "feature", "errata", "F-0001", "--file", errFile, "--dir", projDir); err != nil {
		t.Fatalf("errata: %v\n%s", err, out)
	}
	out, err := execCerne(t, "lint", "--dir", projDir)
	if err != nil || strings.Contains(out, "RESULT-MISSING") {
		t.Fatalf("want lint clean of result findings (err %v):\n%s", err, out)
	}
	after, _ := os.ReadFile(resultPath)
	if string(after) != string(before) {
		t.Fatal("result.md changed")
	}
}
