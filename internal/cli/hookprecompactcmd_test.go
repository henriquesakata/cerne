package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// failingNetTools puts git and ssh fakes first on PATH that leave a marker if
// called; the returned func reports whether any was.
func failingNetTools(t *testing.T) func() bool {
	t.Helper()
	bin := t.TempDir()
	marker := filepath.Join(bin, "called")
	for _, n := range []string{"git", "ssh"} {
		if err := os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\necho "+n+" >> "+marker+"\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() bool { _, err := os.Stat(marker); return err == nil }
}

// boundWork starts W-0001 and binds session sess to it.
func boundWork(t *testing.T, sess string) (projDir, cacheDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	cacheDir = filepath.Dir(repoDir)
	if err := app.BindSession(cacheDir, sess, "W-0001"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}
	return projDir, cacheDir, repoDir
}

func saveHO(t *testing.T, repoDir, id, work, next string, at time.Time) {
	t.Helper()
	if err := store.SaveHandoff(repoDir, store.Handoff{
		ID: id, State: domain.HandoffPending, FromWork: work, Actor: "a", Next: next,
		CreatedAt: at, UpdatedAt: at,
	}); err != nil {
		t.Fatalf("SaveHandoff: %v", err)
	}
}

func compactionLines(t *testing.T, cacheDir, sess string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cacheDir, "compactions", sess))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestHookPrecompactKeepsHandoffCode (F-0368 crit. 11): bound to an active W-
// with a handoff, the PreCompact prints the instruction that the summary keep
// the W-, the latest HO (id + state) and `cerne show HO-…` verbatim, appends
// the instant to <cacheDir>/compactions/<session>, and never touches git, ssh
// or the backend clone.
func TestHookPrecompactKeepsHandoffCode(t *testing.T) {
	projDir, cacheDir, repoDir := boundWork(t, "sess-pc")
	base := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	saveHO(t, repoDir, "HO-0001", "W-0001", "old", base)
	saveHO(t, repoDir, "HO-0002", "W-0001", "rodar a suíte", base.Add(time.Hour))
	saveHO(t, repoDir, "HO-0003", "W-0009", "outra work", base.Add(2*time.Hour))
	before := treeSnapshot(t, repoDir)
	called := failingNetTools(t)

	payload := `{"session_id":"sess-pc","cwd":` + jsonString(projDir) + `,"hook_event_name":"PreCompact","trigger":"auto"}`
	out, err := execCerneStdin(t, payload, "hook", "precompact")
	if err != nil {
		t.Fatalf("hook precompact: %v\n%s", err, out)
	}
	for _, want := range []string{"verbatim", "W-0001", "HO-0002 (pending)", "cerne show HO-0002"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "HO-0003") || strings.Contains(out, "HO-0001") {
		t.Fatalf("output names a handoff that is not the W-'s latest:\n%s", out)
	}
	lines := compactionLines(t, cacheDir, "sess-pc")
	if len(lines) != 1 {
		t.Fatalf("compactions = %v, want one instant", lines)
	}
	id, instant, _ := strings.Cut(lines[0], " ")
	if ts, err := time.Parse(time.RFC3339Nano, instant); id != "W-0001" || err != nil || time.Since(ts) > time.Minute {
		t.Fatalf("compaction instant %q: %v", lines[0], err)
	}
	if _, err := execCerneStdin(t, payload, "hook", "precompact"); err != nil {
		t.Fatal(err)
	}
	if n := len(compactionLines(t, cacheDir, "sess-pc")); n != 2 {
		t.Fatalf("second compaction not appended: %d lines", n)
	}
	if called() {
		t.Fatal("precompact invoked git/ssh")
	}
	if !reflect.DeepEqual(before, treeSnapshot(t, repoDir)) {
		t.Fatal("precompact wrote to the backend clone")
	}
}

// TestHookPrecompactWithoutHandoff (crit. 11): bound with no HO yet, the
// instruction still keeps the W- and says how to record one.
func TestHookPrecompactWithoutHandoff(t *testing.T) {
	projDir, cacheDir, _ := boundWork(t, "sess-pc0")
	out, err := execCerneStdin(t, stopPayload(t, "sess-pc0", projDir), "hook", "precompact")
	if err != nil {
		t.Fatalf("hook precompact: %v\n%s", err, out)
	}
	for _, want := range []string{"verbatim", "W-0001", "no handoff yet", `cerne work handoff W-0001 --next`} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if n := len(compactionLines(t, cacheDir, "sess-pc0")); n != 1 {
		t.Fatalf("compactions = %d, want 1", n)
	}
}

// TestHookPrecompactSilentAndFailOpen (crit. 11): no binding, a bad payload
// and an unsafe session id all exit 0 with no output and no compaction file.
func TestHookPrecompactSilentAndFailOpen(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	cacheDir := filepath.Dir(repoDir)
	for name, payload := range map[string]string{
		"unbound":  stopPayload(t, "sess-none", projDir),
		"bad json": "{not json",
		"empty":    "",
		"unsafe":   stopPayload(t, "../escape", projDir),
		"no cerne": stopPayload(t, "sess-x", t.TempDir()),
	} {
		out, err := execCerneStdin(t, payload, "hook", "precompact")
		if err != nil || strings.TrimSpace(out) != "" {
			t.Fatalf("%s: err=%v out=%q", name, err, out)
		}
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "compactions")); !os.IsNotExist(err) {
		t.Fatalf("compactions dir created without a binding: %v", err)
	}
}

// TestHookPrecompactCorruptStore (crit. 11, critic round 1): an unreadable
// handoff store still exits 0, writes nothing to the backend, keeps the W-
// line and says nothing about the HO it cannot read — "no handoff yet" would
// be a guess.
func TestHookPrecompactCorruptStore(t *testing.T) {
	projDir, _, repoDir := boundWork(t, "sess-bad")
	dir := filepath.Join(repoDir, "entities", "handoffs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HO-0001.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, repoDir)
	out, err := execCerneStdin(t, stopPayload(t, "sess-bad", projDir), "hook", "precompact")
	if err != nil {
		t.Fatalf("hook precompact: %v\n%s", err, out)
	}
	if !strings.Contains(out, "- work: W-0001") || strings.Contains(out, "handoff") {
		t.Fatalf("want the W- line and no HO guess:\n%s", out)
	}
	if !reflect.DeepEqual(before, treeSnapshot(t, repoDir)) {
		t.Fatal("precompact wrote to the backend clone")
	}
}
