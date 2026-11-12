package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func TestShowFeature(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}

	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show F-0001: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"F-0001", "feature", "[candidate]", "Login com Google"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show F-0001 output missing %q:\n%s", want, out)
		}
	}
}

func TestShowEpicAndTask(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "epic", "new", "Épico teste", "--context", "ctx de teste", "--component", "test", "--dir", projDir); err != nil {
		t.Fatalf("epic new: %v", err)
	}
	// The approve bar gate (F-0545) also covers epic specs; the creation-seeded
	// spec has no bar, so give the epic a real one.
	writeDoc(t, repoDir, "E-0001", "spec.md", realDoc("E-0001", "PRD real do épico."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", "E-0001", "--dir", projDir); err != nil {
		t.Fatalf("feature approve E-0001: %v", err)
	}
	if _, err := execCerne(t, "task", "new", "Passo teste", "--parent", "E-0001", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("task new: %v", err)
	}

	out, err := execCerne(t, "show", "E-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show E-0001: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "E-0001") || !strings.Contains(out, "feature") {
		t.Fatalf("show E-0001 output missing expected fields:\n%s", out)
	}

	out, err = execCerne(t, "show", "T-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show T-0001: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "T-0001") || !strings.Contains(out, "feature") {
		t.Fatalf("show T-0001 output missing expected fields:\n%s", out)
	}

	if _, err := execCerne(t, "show", "T-9999", "--dir", projDir); err == nil {
		t.Fatal("show T-9999: expected not-found error, got none")
	}
}

func TestShowNoteWithBody(t *testing.T) {
	projDir, _, _ := setupProject(t)
	body := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(body, []byte("go-git ignora ~/.ssh/config; use a key-file fallback.\n"), 0o644); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if _, err := execCerne(t, "note", "new", "SSH gotcha", "--file", body, "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}

	out, err := execCerne(t, "show", "N-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show N-0001: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "N-0001") || !strings.Contains(out, "note") {
		t.Fatalf("show N-0001 missing header:\n%s", out)
	}
	if !strings.Contains(out, "key-file fallback") {
		t.Fatalf("show N-0001 missing body content:\n%s", out)
	}
}

func TestShowJSON(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}

	out, err := execCerne(t, "show", "F-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --json: %v\noutput: %s", err, out)
	}
	var got struct {
		Kind   string `json:"kind"`
		Entity struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			State string `json:"state"`
		} `json:"entity"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if got.Kind != "feature" || got.Entity.ID != "F-0001" || got.Entity.State != "candidate" {
		t.Fatalf("json = %+v, want feature/F-0001/candidate", got)
	}
}

// seedFeatureDocs writes real spec/plan/result bodies straight into the backend
// clone, standing in for the LLM authoring layer.
func seedFeatureDocs(t *testing.T, repoDir, id string, docs map[string]string) {
	t.Helper()
	files := map[string]string{}
	for name, body := range docs {
		files[filepath.ToSlash(filepath.Join("content", "features", id, name+".md"))] = body
	}
	writeTree(t, repoDir, files)
}

func TestShowFeatureDoc(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}
	seedFeatureDocs(t, repoDir, "F-0001", map[string]string{
		"spec":   "# Spec\n\nO que o login precisa fazer.\n",
		"result": "# Result\n\nDébito: falta rate limit no callback.\n",
	})

	out, err := execCerne(t, "show", "F-0001", "--doc", "result", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --doc result: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "falta rate limit") {
		t.Fatalf("show --doc result missing body:\n%s", out)
	}
	if strings.Contains(out, "O que o login precisa fazer") {
		t.Fatalf("show --doc result leaked the spec:\n%s", out)
	}
	if strings.Contains(out, "[candidate]") || strings.Contains(out, "## result") {
		t.Fatalf("show --doc result should print the raw doc, got:\n%s", out)
	}
}

func TestShowFeatureDocJSON(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}
	seedFeatureDocs(t, repoDir, "F-0001", map[string]string{
		"spec":   "# Spec\n\nO que o login precisa fazer.\n",
		"result": "# Result\n\nDébito: falta rate limit no callback.\n",
	})

	out, err := execCerne(t, "show", "F-0001", "--doc", "result", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --doc result --json: %v\noutput: %s", err, out)
	}
	var got struct {
		Kind    string `json:"kind"`
		Content []struct {
			Name string `json:"name"`
			Body string `json:"body"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if len(got.Content) != 1 || got.Content[0].Name != "result" || !strings.Contains(got.Content[0].Body, "rate limit") {
		t.Fatalf("json content = %+v, want only the result doc", got.Content)
	}
}

func TestShowFeatureDocMissing(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}
	if out, err := execCerne(t, "show", "F-0001", "--doc", "result", "--dir", projDir); err == nil {
		t.Fatalf("show --doc result succeeded on a feature with no result:\n%s", out)
	}
}

func TestShowDocRejectsNonFeature(t *testing.T) {
	projDir, _, _ := setupProject(t)
	body := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(body, []byte("nota de teste\n"), 0o644); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if _, err := execCerne(t, "note", "new", "SSH gotcha", "--file", body, "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
	if out, err := execCerne(t, "show", "N-0001", "--doc", "spec", "--dir", projDir); err == nil {
		t.Fatalf("show --doc on a note succeeded:\n%s", out)
	}
}

func TestShowDocRejectsUnknownName(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}
	if out, err := execCerne(t, "show", "F-0001", "--doc", "resumo", "--dir", projDir); err == nil {
		t.Fatalf("show --doc resumo succeeded:\n%s", out)
	}
}

func TestShowUnknownPrefix(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "show", "Z-0001", "--dir", projDir); err == nil {
		t.Fatal("show Z-0001 succeeded, want unknown-prefix error")
	}
}

func TestShowNotFound(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "show", "F-9999", "--dir", projDir); err == nil {
		t.Fatal("show F-9999 succeeded, want not-found error")
	}
}

// F-0885 crit. 8 (and the CLI of crit. 1 and 3): promote of several pendings
// takes --title and prints one line per pending; link points one more at the
// same feature; show of that feature lists, in a section of its own, every
// pending it pays with the state of each — not the one it never paid, and the
// origin keeps only its own open-debts section.
func TestShowFeatureListsPendingsItPays(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Origem")
	pendingNew(t, projDir, "Débito A", "--from", "F-0001")
	pendingNew(t, projDir, "Débito B", "--from", "F-0001")
	pendingNew(t, projDir, "Débito ligado depois", "--from", "F-0001")
	pendingNew(t, projDir, "Débito de fora", "--from", "F-0001")
	if _, err := execCerne(t, "pending", "promote", "P-0001", "P-0002", "--as", "feature", "--dir", projDir); !errors.Is(err, app.ErrPendingGroupTitle) {
		t.Fatalf("promote of two without --title: err = %v, want ErrPendingGroupTitle", err)
	}
	out, err := execCerne(t, "pending", "promote", "P-0001", "P-0002", "--as", "feature", "--title", "Pagar A e B", "--dir", projDir)
	if err != nil || !strings.Contains(out, "P-0001: open -> promoted (F-0002)\nP-0002: open -> promoted (F-0002)") {
		t.Fatalf("promote of two: %v\n%s", err, out)
	}
	if f, err := store.LoadFeature(repoDir, "F-0002"); err != nil || f.Title != "Pagar A e B" {
		t.Fatalf("F-0002 = %+v, %v; want the --title", f, err)
	}
	out, err = execCerne(t, "pending", "link", "P-0003", "F-0002", "--dir", projDir)
	if err != nil || !strings.Contains(out, "P-0003: open -> promoted (F-0002)") {
		t.Fatalf("link: %v\n%s", err, out)
	}
	// A pending already paid keeps pointing at the feature: its state shows.
	p, err := store.LoadPending(repoDir, "P-0003")
	if err != nil {
		t.Fatal(err)
	}
	p.State = domain.PendingResolved
	if err := store.SavePending(repoDir, p); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)

	out, err = execCerne(t, "show", "F-0002", "--dir", projDir)
	if err != nil {
		t.Fatalf("show F-0002: %v\n%s", err, out)
	}
	want := "## pays\nP-0001  [promoted]  Débito A\nP-0002  [promoted]  Débito B\nP-0003  [resolved]  Débito ligado depois\n"
	if !strings.Contains(out, want) || strings.Contains(out, "P-0004") {
		t.Fatalf("show F-0002 must list the three it pays, with their states, and not P-0004:\n%s", out)
	}
	out, err = execCerne(t, "show", "F-0002", "--json", "--dir", projDir)
	if err != nil || !strings.Contains(out, `"name": "pays"`) {
		t.Fatalf("show --json lacks the pays section: %v\n%s", err, out)
	}
	out, err = execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil || strings.Contains(out, "## pays") || !strings.Contains(out, "## pending\nP-0004  Débito de fora") {
		t.Fatalf("show F-0001 must keep only its open debt P-0004, with no pays section: %v\n%s", err, out)
	}
}

// F-0885 crit. 8 (and the CLI of crit. 4): show of a linked pending names the
// paying feature with its state, text and json; unlink brings it back to open
// and the forward link goes.
func TestShowPendingNamesTargetState(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newOriginFeature(t, projDir, "Origem")
	newOriginFeature(t, projDir, "Quem paga")
	pendingNew(t, projDir, "Débito", "--from", "F-0001")
	if out, err := execCerne(t, "pending", "link", "P-0001", "F-0002", "--dir", projDir); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	out, err := execCerne(t, "show", "P-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "promoted to: F-0002 (candidate)\n") {
		t.Fatalf("show P-0001 lacks the target's state: %v\n%s", err, out)
	}
	out, err = execCerne(t, "show", "P-0001", "--json", "--dir", projDir)
	var doc struct {
		Entity struct {
			PromotedTo      string `json:"promoted_to"`
			PromotedToState string `json:"promoted_to_state"`
		} `json:"entity"`
	}
	if err != nil || json.Unmarshal([]byte(out), &doc) != nil || doc.Entity.PromotedTo != "F-0002" || doc.Entity.PromotedToState != "candidate" {
		t.Fatalf("show --json = %+v, %v; want promoted_to F-0002 (candidate)\n%s", doc, err, out)
	}
	out, err = execCerne(t, "pending", "unlink", "P-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "P-0001: promoted -> open") {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	out, err = execCerne(t, "show", "P-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "[open]") || strings.Contains(out, "promoted to:") {
		t.Fatalf("show after unlink: %v\n%s", err, out)
	}
}

// amendDoc runs `cerne feature amend id --doc doc` with body and reason.
func amendDoc(t *testing.T, projDir, id, doc, body, reason string) {
	t.Helper()
	if out, err := execCerne(t, "feature", "amend", id, "--doc", doc, "--file", bodyFile(t, body), "--reason", reason, "--dir", projDir); err != nil {
		t.Fatalf("amend %s %s: %v\n%s", id, doc, err, out)
	}
}

// TestShowAmendments (F-0338 crit. 8): with no revision there is no
// `## amendments` block nor JSON field; after a spec and a plan amend the
// current documents print once, followed — before the errata — by one line per
// revision, newest first, and the hint to read one; no replaced body prints,
// and --json carries the same lines under amendments.
func TestShowAmendments(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedDocFeature(t, repoDir, "F-0001", domain.StateApproved, realDoc("F-0001", "Spec original."), realDoc("F-0001", "Plano original."))
	if err := store.AppendErrata(repoDir, "F-0001", "Uma errata.", "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)

	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil || strings.Contains(out, "## amendments") {
		t.Fatalf("no revision: err %v, want no amendments block:\n%s", err, out)
	}
	if js, _ := execCerne(t, "show", "F-0001", "--json", "--dir", projDir); strings.Contains(js, `"amendments"`) {
		t.Fatalf("no revision: JSON carries amendments:\n%s", js)
	}

	amendDoc(t, projDir, "F-0001", "spec", realDoc("F-0001", "Spec vigente."), "prefixo mudou")
	amendDoc(t, projDir, "F-0001", "plan", realDoc("F-0001", "Plano vigente."), "auditoria")
	var lines []string
	evs, _ := store.ReadEvents(repoDir)
	for i := len(evs) - 1; i >= 0; i-- {
		if ev := evs[i]; ev.Type == "feature.doc_edited" && ev.Payload["rev"] != "" {
			lines = append(lines, ev.Payload["doc"]+" r"+ev.Payload["rev"]+" · "+ev.At.Format(time.DateOnly)+" · "+ev.Actor+" · "+ev.Payload["reason"])
		}
	}
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "plan r1 · ") || !strings.HasPrefix(lines[1], "spec r1 · ") {
		t.Fatalf("events = %v, want the plan amend then the spec amend", lines)
	}

	out, err = execCerne(t, "show", "F-0001", "--all", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	block := "\n## amendments\n" + lines[0] + "\n" + lines[1] + "\nread one: cerne show F-0001 --doc <doc> --rev N\n"
	plan, amend, errata := strings.Index(out, "Plano vigente."), strings.Index(out, block), strings.Index(out, "\n## errata\n")
	if amend < 0 || plan < 0 || errata < 0 || !(plan < amend && amend < errata) {
		t.Fatalf("want the documents, then\n%s\nthen the errata; got:\n%s", block, out)
	}
	if strings.Contains(out, "Spec original.") || strings.Contains(out, "Plano original.") || strings.Count(out, "Spec vigente.") != 1 {
		t.Fatalf("show must print the current documents once and no replaced body:\n%s", out)
	}

	js, err := execCerne(t, "show", "F-0001", "--json", "--dir", projDir)
	var doc struct {
		Content []struct {
			Name string `json:"name"`
		} `json:"content"`
		Amendments []struct {
			Doc    string    `json:"doc"`
			Rev    int       `json:"rev"`
			At     time.Time `json:"at"`
			Actor  string    `json:"actor"`
			Reason string    `json:"reason"`
		} `json:"amendments"`
	}
	if err != nil || json.Unmarshal([]byte(js), &doc) != nil {
		t.Fatalf("show --json: %v\n%s", err, js)
	}
	if len(doc.Amendments) != 2 || doc.Amendments[0].Doc != "plan" || doc.Amendments[0].Rev != 1 || doc.Amendments[0].Reason != "auditoria" ||
		doc.Amendments[1].Doc != "spec" || doc.Amendments[1].Actor == "" || doc.Amendments[1].At.IsZero() {
		t.Fatalf("amendments = %+v, want plan r1 then spec r1", doc.Amendments)
	}
	for _, c := range doc.Content {
		if c.Name == "amendments" {
			t.Fatalf("content repeats the amendments block: %s", js)
		}
	}
}

// TestShowFeatureDocRev (F-0338 crit. 9): `--doc spec|plan --rev N` prints
// revision N of that document byte for byte (K+1 is the current one); an N
// outside the range is refused naming it, and on a feature --rev needs --doc
// spec or --doc plan (and is text, not --json).
func TestShowFeatureDocRev(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	original := realDoc("F-0001", "Spec original.")
	seedDocFeature(t, repoDir, "F-0001", domain.StateApproved, original, realDoc("F-0001", "Plano original."))
	amendDoc(t, projDir, "F-0001", "spec", realDoc("F-0001", "Spec vigente."), "prefixo mudou")

	if out, err := execCerne(t, "show", "F-0001", "--doc", "spec", "--rev", "1", "--dir", projDir); err != nil || out != original {
		t.Fatalf("--rev 1 = %q, %v — want the original spec byte for byte %q", out, err, original)
	}
	current, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if out, err := execCerne(t, "show", "F-0001", "--doc", "spec", "--rev", "2", "--dir", projDir); err != nil || out != string(current) {
		t.Fatalf("--rev 2 = %q, %v — want the current spec", out, err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--doc", "spec", "--rev", "3"}, "F-0001 spec has revisions 1..2, no revision 3"},
		{[]string{"--doc", "plan", "--rev", "2"}, "F-0001 plan has revisions 1..1, no revision 2"},
		{[]string{"--rev", "1"}, "pass --doc spec or --doc plan"},
		{[]string{"--doc", "result", "--rev", "1"}, "pass --doc spec or --doc plan"},
		{[]string{"--doc", "spec", "--rev", "1", "--json"}, "--json"},
	} {
		out, err := execCerne(t, append([]string{"show", "F-0001", "--dir", projDir}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: err %v, want %q\n%s", tc.args, err, tc.want, out)
		}
	}
}

// TestShowFeatureDocRevEdges (F-0338 crit. 9, round 2): --rev 0 or negative is
// refused with the valid range, --rev with --all is incompatible, and a plan
// that does not exist says so instead of a raw os error.
func TestShowFeatureDocRevEdges(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedDocFeature(t, repoDir, "F-0001", domain.StateApproved, realDoc("F-0001", "Spec."), "")
	_ = os.Remove(filepath.Join(repoDir, "content", "features", "F-0001", "plan.md"))
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--doc", "spec", "--rev", "0"}, "revisions 1..1"},
		{[]string{"--doc", "spec", "--rev", "-1"}, "revisions 1..1"},
		{[]string{"--doc", "spec", "--rev", "1", "--all"}, "--all"},
		{[]string{"--doc", "plan", "--rev", "1"}, "has no plan"},
	} {
		out, err := execCerne(t, append([]string{"show", "F-0001", "--dir", projDir}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "no such file") {
			t.Fatalf("%v: err %v, want %q\n%s", tc.args, err, tc.want, out)
		}
	}
}

// seedShowGolden seeds the feature of the show golden: in development, with a
// spec, a plan and a result, an erratum and an open pending — the documents
// and the blocks a feature show prints, and no revision.
func seedShowGolden(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	seedDocFeature(t, repoDir, "F-0001", domain.StateInDevelopment, realDoc("F-0001", "A spec da feature."), realDoc("F-0001", "O plano da feature."))
	writeDoc(t, repoDir, "F-0001", "result.md", realDoc("F-0001", "O result da feature."))
	if err := store.AppendErrata(repoDir, "F-0001", "Uma errata.", "x", time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	pendingNew(t, projDir, "Débito aberto", "--from", "F-0001")
	return projDir, repoDir
}

// TestShowGoldenAll (F-0338 crit. 15): --all prints the full show of a
// feature with the three documents and no revision, byte for byte against
// testdata/show_all.golden — recorded from the show of before the stage view,
// so the full output stays provably what it was.
func TestShowGoldenAll(t *testing.T) {
	projDir, _ := seedShowGolden(t)
	out, err := execCerne(t, "show", "F-0001", "--all", "--dir", projDir)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "show_all.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if out != string(want) {
		t.Fatalf("show differs from testdata/show_all.golden:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

// otherDocsLine is the closing line of a feature's stage view (F-0338 crit. 14).
func otherDocsLine(id string, docs ...string) string {
	return "other documents: " + strings.Join(docs, " · ") + " — cerne show " + id + " --doc <doc> | --all"
}

// docSize is how the other-documents line names a document: name (N lines).
func docSize(name, body string) string {
	return fmt.Sprintf("%s (%d lines)", name, strings.Count(strings.TrimRight(body, "\n"), "\n")+1)
}

// TestShowStageDoc (F-0338 crit. 14): without --doc and --all, a feature's show
// prints one document — the spec in candidate, approved, rejected and
// superseded, the plan in development, the result once implemented or done —
// and closes with the line naming the others, with their sizes.
func TestShowStageDoc(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	docs := map[string]string{"spec": realDoc("F-0001", "Corpo da spec."), "plan": realDoc("F-0001", "Corpo do plano.\n\nCom mais linhas."), "result": realDoc("F-0001", "Corpo do result.")}
	seedFeatureDocs(t, repoDir, "F-0001", docs)
	for _, tc := range []struct {
		state domain.FeatureState
		doc   string
	}{
		{domain.StateCandidate, "spec"},
		{domain.StateApproved, "spec"},
		{domain.StateInDevelopment, "plan"},
		{domain.StateImplemented, "result"},
		{domain.StateDone, "result"},
		{domain.StateRejected, "spec"},
		{domain.StateSuperseded, "spec"},
	} {
		if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Feature F-0001", State: tc.state, Facets: []string{"code"}}); err != nil {
			t.Fatal(err)
		}
		commitBackend(t, repoDir)
		out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
		if err != nil {
			t.Fatalf("%s: %v\n%s", tc.state, err, out)
		}
		var others []string
		for _, name := range featureDocNames {
			if name != tc.doc {
				others = append(others, docSize(name, docs[name]))
				if strings.Contains(out, "\n## "+name+"\n") {
					t.Fatalf("%s: printed the %s too:\n%s", tc.state, name, out)
				}
			}
		}
		want := "F-0001  feature  [" + string(tc.state) + "]  Feature F-0001\n\n## " + tc.doc + "\n" + strings.TrimRight(docs[tc.doc], "\n") + "\n\n" + otherDocsLine("F-0001", others...) + "\n"
		if out != want {
			t.Fatalf("%s: show =\n%s\nwant\n%s", tc.state, out, want)
		}
	}
}

// TestShowStageDocPastStub (F-0338 crit. 14): the stage document still a stub
// (or absent) falls back to the most advanced real one before it; a stub is
// never listed, and a real document past the stage is listed, not printed.
func TestShowStageDocPastStub(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	spec, result := realDoc("F-0001", "Corpo da spec."), realDoc("F-0001", "Corpo do result.")
	seedDocFeature(t, repoDir, "F-0001", domain.StateInDevelopment, spec, "---\ncerne_id: F-0001\n---\n\n# Plano\n\n## Estratégia\n<!-- guia -->\n")
	writeDoc(t, repoDir, "F-0001", "result.md", result)
	commitBackend(t, repoDir)
	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "\n## spec\n") || strings.Contains(out, "\n## result\n") || strings.Contains(out, "plan (") ||
		!strings.HasSuffix(out, "\n"+otherDocsLine("F-0001", docSize("result", result))+"\n") {
		t.Fatalf("in development with a stub plan: want the spec, the result listed: %v\n%s", err, out)
	}

	plan := realDoc("F-0002", "Corpo do plano.")
	seedDocFeature(t, repoDir, "F-0002", domain.StateImplemented, realDoc("F-0002", "Spec dois."), plan)
	out, err = execCerne(t, "show", "F-0002", "--dir", projDir)
	if err != nil || !strings.Contains(out, "\n## plan\n") || strings.Contains(out, "\n## spec\n") ||
		!strings.HasSuffix(out, "\n"+otherDocsLine("F-0002", docSize("spec", realDoc("F-0002", "Spec dois.")))+"\n") {
		t.Fatalf("implemented without a result: want the plan, the spec listed: %v\n%s", err, out)
	}
}

// TestShowStageViewKeepsBlocks (F-0338 crit. 14, 15): the stage view is the full
// show minus the other documents — header and blocks byte-equal — closed by
// the other-documents line; --json still carries every document; and --all
// changes nothing for a non-feature.
func TestShowStageViewKeepsBlocks(t *testing.T) {
	projDir, repoDir := seedShowGolden(t)
	full, err := os.ReadFile(filepath.Join("testdata", "show_all.golden"))
	if err != nil {
		t.Fatal(err)
	}
	g := string(full)
	spec, plan, result, errata := strings.Index(g, "\n## spec\n"), strings.Index(g, "\n## plan\n"), strings.Index(g, "\n## result\n"), strings.Index(g, "\n## errata\n")
	var sizes []string
	for _, name := range []string{"spec", "result"} {
		body, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", name+".md"))
		sizes = append(sizes, docSize(name, string(body)))
	}
	want := g[:spec] + g[plan:result] + g[errata:] + "\n" + otherDocsLine("F-0001", sizes...) + "\n"
	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil || out != want {
		t.Fatalf("stage view (%v) =\n%s\nwant\n%s", err, out, want)
	}

	js, err := execCerne(t, "show", "F-0001", "--json", "--dir", projDir)
	var doc struct {
		Content []struct {
			Name string `json:"name"`
		} `json:"content"`
	}
	if err != nil || json.Unmarshal([]byte(js), &doc) != nil {
		t.Fatalf("show --json: %v\n%s", err, js)
	}
	var names []string
	for _, c := range doc.Content {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "spec,plan,result,errata,pending" {
		t.Fatalf("--json content = %v, want every document and block", names)
	}

	if out, err := execCerne(t, "note", "new", "Nota", "--file", bodyFile(t, "Corpo da nota.\n"), "--dir", projDir); err != nil {
		t.Fatalf("note new: %v\n%s", err, out)
	}
	plain, err1 := execCerne(t, "show", "N-0001", "--dir", projDir)
	all, err2 := execCerne(t, "show", "N-0001", "--all", "--dir", projDir)
	if err1 != nil || err2 != nil || plain != all {
		t.Fatalf("--all on a note: %v %v\n%s\nvs\n%s", err1, err2, plain, all)
	}
}
