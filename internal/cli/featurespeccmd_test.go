package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// specBody is the revised spec used across the feature-spec tests.
const specBody = "---\ncerne_id: F-0001\n---\n\n## Problema\n\nVersão revisada pelo agente.\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\n"

// pipeStdinBody feeds body to readOptionalBody as if it had been piped in —
// the path the Backlog agent actually uses (it has no Write tool, so the spec
// arrives on stdin via heredoc, F-0265).
func pipeStdinBody(t *testing.T, body string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(body); err != nil {
		t.Fatal(err)
	}
	w.Close()
	t.Cleanup(func() { r.Close() })
	swapStdinBody(t, r)
}

// TestFeatureSpecFromStdin (F-0265 criterion 1): the verb rewrites a
// candidate's spec.md from a piped body, through the F-0256 core.
func TestFeatureSpecFromStdin(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}

	pipeStdinBody(t, specBody)
	out, err := execCerne(t, "feature", "spec", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature spec: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001 spec updated") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}
	got, err := os.ReadFile(app.FeatureDocPath(repoDir, "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if !strings.Contains(string(got), "Versão revisada pelo agente.") {
		t.Fatalf("spec not rewritten:\n%s", got)
	}
}

// TestFeatureSpecFromFile (criterion 1): --file is the human-side entry point.
func TestFeatureSpecFromFile(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	path := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(path, []byte(specBody), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, err := execCerne(t, "feature", "spec", "F-0001", "--file", path, "--dir", projDir); err != nil {
		t.Fatalf("feature spec --file: %v\noutput: %s", err, out)
	}
	got, err := os.ReadFile(app.FeatureDocPath(repoDir, "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if !strings.Contains(string(got), "Versão revisada pelo agente.") {
		t.Fatalf("spec not rewritten:\n%s", got)
	}
}

// TestFeatureSpecRefusals pins the guards: past approval (criterion 2, moved
// by F-0318), empty
// body (criterion 3) and unknown id named in the error (criterion 11).
func TestFeatureSpecRefusals(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}

	t.Run("empty body", func(t *testing.T) {
		pipeStdinBody(t, "   \n")
		_, err := execCerne(t, "feature", "spec", "F-0001", "--dir", projDir)
		if err == nil || !strings.Contains(err.Error(), "empty spec body") {
			t.Fatalf("err = %v, want empty-body refusal", err)
		}
		got, _ := os.ReadFile(app.FeatureDocPath(repoDir, "F-0001", "spec.md"))
		if strings.Contains(string(got), "Versão revisada") {
			t.Fatal("empty body must not write")
		}
	})

	t.Run("unknown id", func(t *testing.T) {
		pipeStdinBody(t, specBody)
		_, err := execCerne(t, "feature", "spec", "F-9999", "--dir", projDir)
		if !errors.Is(err, store.ErrNotFound) || !strings.Contains(err.Error(), "F-9999") {
			t.Fatalf("err = %v, want ErrNotFound naming F-9999", err)
		}
	})

	t.Run("in development", func(t *testing.T) {
		// The creation-seeded spec has no Gauntlet bar; give it one so the
		// approve below exercises ErrNotCandidate, not the bar gate (F-0545).
		// An approved spec is open to the verb since F-0318; in development not.
		writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Spec aprovável."))
		writeDoc(t, repoDir, "F-0001", "plan.md", realDoc("F-0001", "Plano real."))
		commitBackend(t, repoDir)
		for _, action := range []string{"approve", "start"} {
			if out, err := execCerne(t, "feature", action, "F-0001", "--dir", projDir); err != nil {
				t.Fatalf("%s: %v\noutput: %s", action, err, out)
			}
		}
		pipeStdinBody(t, specBody)
		_, err := execCerne(t, "feature", "spec", "F-0001", "--dir", projDir)
		if !errors.Is(err, app.ErrNotCandidate) {
			t.Fatalf("err = %v, want ErrNotCandidate", err)
		}
		got, _ := os.ReadFile(app.FeatureDocPath(repoDir, "F-0001", "spec.md"))
		if strings.Contains(string(got), "Versão revisada") {
			t.Fatal("a spec in development must not be rewritten by the verb")
		}
	})
}

// TestFeatureSpecKeepsOneFrontmatter (F-0328 crit. 1): the natural revision —
// start from `cerne show --doc spec`, edit, write it back — stores ONE block,
// the body's own; a quoted id is stored unquoted and an extra key is kept.
func TestFeatureSpecKeepsOneFrontmatter(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	path := filepath.Join(t.TempDir(), "spec.md")
	specPath := app.FeatureDocPath(repoDir, "F-0001", "spec.md")
	write := func(body string) string {
		t.Helper()
		mustWrite(t, path, body)
		if out, err := execCerne(t, "feature", "spec", "F-0001", "--file", path, "--dir", projDir); err != nil {
			t.Fatalf("feature spec: %v\noutput: %s", err, out)
		}
		got, _ := os.ReadFile(specPath)
		return string(got)
	}

	if got := write(specBody); got != specBody {
		t.Fatalf("spec.md = %q, want the body's own single block %q", got, specBody)
	}
	shown, err := execCerne(t, "show", "F-0001", "--doc", "spec", "--dir", projDir)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, shown)
	}
	got := write(strings.Replace(shown, "Versão revisada pelo agente.", "Revista a partir do show.", 1))
	if strings.Count(got, "cerne_id:") != 1 || !strings.HasPrefix(got, "---\ncerne_id: F-0001\n---\n") || !strings.Contains(got, "Revista a partir do show.") {
		t.Fatalf("spec.md after a show round trip = %q, want one block and the edit", got)
	}

	want := "---\ncerne_id: F-0001\nowner: x\n---\n\n## Problema\n\nCom chave extra.\n"
	if got := write("---\ncerne_id: \"F-0001\"\nowner: x\n---\n\n## Problema\n\nCom chave extra.\n"); got != want {
		t.Fatalf("spec.md = %q, want %q", got, want)
	}
}

// TestFeatureSpecRefusesForeignFrontmatter (F-0328 crit. 2-3): a block naming
// another feature, a block without cerne_id and a block that never closes are
// refused before anything is written, the error saying which — spec.md, the
// event log and origin all left as they were.
func TestFeatureSpecRefusesForeignFrontmatter(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	specPath := app.FeatureDocPath(repoDir, "F-0001", "spec.md")
	before, _ := os.ReadFile(specPath)
	evs0, _ := store.ReadEvents(repoDir)
	head0, _ := gitback.HeadSHA(origin)

	for _, tc := range []struct {
		name, body string
		want       []string
	}{
		{"other id", "---\ncerne_id: F-0002\n---\n\n## Problema\n\nDe outra.\n", []string{`cerne_id "F-0002", not F-0001`, "another feature's spec"}},
		{"block without id", "---\ntitle: x\n---\n\n## Problema\n\nSem id.\n", []string{"without cerne_id", "--doc spec"}},
		{"block never closes", "---\ncerne_id: F-0001\n\n## Problema\n\nAberto.\n", []string{"never closes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "spec.md")
			mustWrite(t, path, tc.body)
			_, err := execCerne(t, "feature", "spec", "F-0001", "--file", path, "--dir", projDir)
			if !errors.Is(err, app.ErrBodyFrontmatter) {
				t.Fatalf("err = %v, want ErrBodyFrontmatter", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("err = %v, want it to say %q", err, w)
				}
			}
			if after, _ := os.ReadFile(specPath); string(after) != string(before) {
				t.Fatalf("spec.md changed on a refusal: %q", after)
			}
			if evs, _ := store.ReadEvents(repoDir); len(evs) != len(evs0) {
				t.Fatalf("events = %d, want %d", len(evs), len(evs0))
			}
			if h, _ := gitback.HeadSHA(origin); h != head0 {
				t.Fatalf("origin moved on a refusal: %s -> %s", head0, h)
			}
		})
	}
}

// TestFeatureSpecIdenticalSaysUnchanged (F-0328 crit. 4): writing back what
// `cerne show --doc spec` printed is a no-op the CLI names — no event, no
// commit, no "spec updated".
func TestFeatureSpecIdenticalSaysUnchanged(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	path := filepath.Join(t.TempDir(), "spec.md")
	mustWrite(t, path, specBody)
	if out, err := execCerne(t, "feature", "spec", "F-0001", "--file", path, "--dir", projDir); err != nil || !strings.Contains(out, "F-0001 spec updated") {
		t.Fatalf("first write: %v\noutput: %s", err, out)
	}
	shown, err := execCerne(t, "show", "F-0001", "--doc", "spec", "--dir", projDir)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, shown)
	}
	mustWrite(t, path, shown)
	evs0, _ := store.ReadEvents(repoDir)
	head0, _ := gitback.HeadSHA(origin)

	out, err := execCerne(t, "feature", "spec", "F-0001", "--file", path, "--dir", projDir)
	if err != nil || !strings.Contains(out, "F-0001 spec unchanged — nothing to update.") || strings.Contains(out, "spec updated") {
		t.Fatalf("identical rewrite: err %v, output %q — want the unchanged line only", err, out)
	}
	if evs, _ := store.ReadEvents(repoDir); len(evs) != len(evs0) {
		t.Fatalf("events = %d, want %d", len(evs), len(evs0))
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved: %s -> %s", head0, h)
	}
}
