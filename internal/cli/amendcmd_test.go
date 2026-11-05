package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// seedResultFeatures persists F-0001.. straight into the backend, one per
// state, each with a real spec and result, and returns the id per state.
func seedResultFeatures(t *testing.T, repoDir string, states ...domain.FeatureState) map[domain.FeatureState]string {
	t.Helper()
	now := time.Now().UTC()
	ids := map[domain.FeatureState]string{}
	for i, s := range states {
		id := fmt.Sprintf("F-%04d", i+1)
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Feature " + id, State: s, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Spec."))
		writeDoc(t, repoDir, id, "result.md", realDoc(id, "Result original."))
		ids[s] = id
	}
	commitBackend(t, repoDir)
	return ids
}

// amendFile writes a result body that passes the result checks and returns its path.
func amendFile(t *testing.T, id string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "result.md")
	if err := os.WriteFile(p, []byte(realDoc(id, "Result emendado.")), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// assertAmendRefused runs `feature amend` on id and asserts the refusal names
// want and leaves the backend result byte-identical.
func assertAmendRefused(t *testing.T, projDir, repoDir, id, want string) error {
	t.Helper()
	resultPath := filepath.Join(repoDir, "content", "features", id, "result.md")
	before, _ := os.ReadFile(resultPath)
	_, err := execCerne(t, "feature", "amend", id, "--file", amendFile(t, id), "--reason", "corrigir", "--dir", projDir)
	if !errors.Is(err, app.ErrAmendNotImplemented) || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s: err = %v, want ErrAmendNotImplemented naming %q", id, err, want)
	}
	if after, _ := os.ReadFile(resultPath); string(after) != string(before) {
		t.Fatalf("%s: result.md changed", id)
	}
	return err
}

// TestAmendRefusesOtherStates (F-0305 crit. 9): outside implemented the amend
// refuses without writing, naming the state's path — pull+push while live,
// errata once closed.
func TestAmendRefusesOtherStates(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ids := seedResultFeatures(t, repoDir, domain.StateCandidate, domain.StateApproved, domain.StateInDevelopment,
		domain.StateDone, domain.StateRejected, domain.StateSuperseded)
	for state, id := range ids {
		want := wantFix(state, id, "result")
		err := assertAmendRefused(t, projDir, repoDir, id, want)
		if state == domain.StateDone {
			t.Logf("amend refusal (done): %v", err)
		}
	}
}

// TestAmendRequiresReasonAndBody (F-0305 crit. 11): no reason, a blank reason
// or an empty body is refused before the backend is touched.
func TestAmendRequiresReasonAndBody(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := seedResultFeatures(t, repoDir, domain.StateImplemented)[domain.StateImplemented]
	resultPath := filepath.Join(repoDir, "content", "features", id, "result.md")
	before, _ := os.ReadFile(resultPath)
	head0, _ := gitback.HeadSHA(origin)
	empty := filepath.Join(t.TempDir(), "empty.md")
	if err := os.WriteFile(empty, []byte(" \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, want string
		args       []string
	}{
		{"no reason", "needs a reason", []string{"--file", amendFile(t, id)}},
		{"blank reason", "needs a reason", []string{"--file", amendFile(t, id), "--reason", "  "}},
		{"empty body", "empty result body", []string{"--file", empty, "--reason", "corrigir"}},
	} {
		_, err := execCerne(t, append([]string{"feature", "amend", id, "--dir", projDir}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want a refusal containing %q", tc.name, err, tc.want)
		}
	}
	if after, _ := os.ReadFile(resultPath); string(after) != string(before) {
		t.Fatal("result.md changed")
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved: %s -> %s", head0, h)
	}
}

// TestFeatureAmendCommand (F-0305 crit. 8, CLI side): the verb replaces the
// result of an implemented feature and says how to read it back.
func TestFeatureAmendCommand(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := seedResultFeatures(t, repoDir, domain.StateImplemented)[domain.StateImplemented]
	out, err := execCerne(t, "feature", "amend", id, "--file", amendFile(t, id), "--reason", "faltava o critério", "--dir", projDir)
	if err != nil {
		t.Fatalf("amend: %v\n%s", err, out)
	}
	t.Logf("amend output: %s", out)
	want := "✓ " + id + " result amended (implemented) — already synced. Read it with 'cerne show " + id + " --doc result'."
	if !strings.Contains(out, want) {
		t.Fatalf("output = %q, want %q", out, want)
	}
	got, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "result.md"))
	if !strings.Contains(string(got), "Result emendado.") {
		t.Fatalf("result not replaced:\n%s", got)
	}

	// The same body again is a no-op, and says so.
	out, err = execCerne(t, "feature", "amend", id, "--file", amendFile(t, id), "--reason", "de novo", "--dir", projDir)
	if want := id + " result unchanged — nothing to amend."; err != nil || !strings.Contains(out, want) || strings.Contains(out, "amended") {
		t.Fatalf("identical amend: err %v, output = %q, want %q", err, out, want)
	}
}

// TestFeatureAmendFromStdinAndUnknownID: the documented `cat result.md |` form
// amends, and an unknown id is refused without moving origin (judge finding,
// F-0305).
func TestFeatureAmendFromStdinAndUnknownID(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := seedResultFeatures(t, repoDir, domain.StateImplemented)[domain.StateImplemented]
	body, _ := os.ReadFile(amendFile(t, id))
	pipeStdinBody(t, string(body))
	if out, err := execCerne(t, "feature", "amend", id, "--reason", "via stdin", "--dir", projDir); err != nil || !strings.Contains(out, "result amended") {
		t.Fatalf("stdin amend: err %v, output %q", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "result.md")); !strings.Contains(string(got), "Result emendado.") {
		t.Fatalf("result not replaced from stdin:\n%s", got)
	}

	head0, _ := gitback.HeadSHA(origin)
	if _, err := execCerne(t, "feature", "amend", "F-0999", "--file", amendFile(t, "F-0999"), "--reason", "x", "--dir", projDir); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: err = %v, want store.ErrNotFound", err)
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved on an unknown id: %s -> %s", head0, h)
	}
}

// F-0307: an amend whose --file is the .docs/ copy says to remove it — the
// copy keeps blocking push until it is gone — on both the success and the
// unchanged message; a file elsewhere gets no such line.
func TestAmendFromDocsCopySaysToRemoveIt(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := seedResultFeatures(t, repoDir, domain.StateImplemented)[domain.StateImplemented]
	local := filepath.Join(projDir, ".docs", "current", "features", id+"-x", "result.md")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, local, realDoc(id, "Result emendado."))
	rel := ".docs/current/features/" + id + "-x/result.md"
	want := "remove " + rel + " now"

	for _, step := range []string{"amended", "unchanged"} {
		out, err := execCerne(t, "feature", "amend", id, "--file", local, "--reason", "r", "--dir", projDir)
		if err != nil || !strings.Contains(out, step) || !strings.Contains(out, want) {
			t.Fatalf("%s: err %v, output %q, want %q", step, err, out, want)
		}
	}
	other := amendFile(t, id)
	for i := 0; i < 2; i++ {
		out, err := execCerne(t, "feature", "amend", id, "--file", other, "--reason", "r", "--dir", projDir)
		if err != nil || strings.Contains(out, "remove ") {
			t.Fatalf("file outside .docs/: err %v, output %q, want no remove line", err, out)
		}
	}
}
