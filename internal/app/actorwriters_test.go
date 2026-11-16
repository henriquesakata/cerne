package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

var storedActorAt = regexp.MustCompile(`"(actor|taken_by|recorded_by)":\s*"[^"]*@|(?m)^## (handoff|errata) .* — .*@`)

// F-0442 crit. 3: every writer of an actor field (work start, runbook run,
// handoff create and take, auto work, event, work log, errata) records through
// the one rule — with a *.local host and an explicit "x@y", no stored actor
// contains "@".
func TestEveryActorWriterStoresNoAt(t *testing.T) {
	for _, env := range []string{"", "agent", "fulano", "agent:bob@x.local", "bob@x.local"} {
		t.Run("env="+env, func(t *testing.T) {
			pinMachine(t, "alice", "mac.local")
			t.Setenv("CERNE_ACTOR", env)
			t.Setenv("CURSOR_AGENT", "")
			t.Setenv("CERNE_SESSION", "")
			repoDir, projDir, _ := newFeatureBackend(t)
			seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
			if err := store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0001", Title: "Rb", State: domain.RunbookActive}); err != nil {
				t.Fatal(err)
			}
			commitBackend(t, repoDir)
			// auto work + feature events
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
				t.Fatalf("feature start: %v", err)
			}
			// work start: default and explicit "x@y" / "agent:x@y"
			var first string
			for _, flag := range []string{"", "x@y", "agent:x@y"} {
				w, err := StartWork(repoDir, projDir, "Atividade "+flag, nil, flag, true, nil, "")
				if err != nil {
					t.Fatalf("StartWork(%q): %v", flag, err)
				}
				if first == "" {
					first = w.ID
				}
			}
			// runbook run: default and explicit
			for _, flag := range []string{"", "x@y"} {
				if _, _, err := StartRun(repoDir, projDir, "RB-0001", "J1"+strings.ReplaceAll(flag, "@", ""), []string{"S1"}, "", flag); err != nil {
					t.Fatalf("StartRun(%q): %v", flag, err)
				}
			}
			// handoff create + take
			id, err := HandoffWork(repoDir, projDir, first, "brief", "next", nil, nil)
			if err != nil {
				t.Fatalf("HandoffWork: %v", err)
			}
			if _, err := TakeHandoff(repoDir, projDir, filepath.Dir(repoDir), "sess", id, "", ""); err != nil {
				t.Fatalf("TakeHandoff: %v", err)
			}
			// work log: an explicit personal actor is refused; the default records
			if _, err := AppendWorkLog(repoDir, projDir, first, "x@y", "texto", 0, time.Now()); err == nil {
				t.Error("work log accepted an e-mail-shaped --actor")
			}
			// errata author goes through the same store rule
			if err := store.AppendErrata(repoDir, "F-0001", "corrigido", "x@y", time.Now()); err != nil {
				t.Fatal(err)
			}
			scanForActorAt(t, repoDir)
		})
	}
}

func scanForActorAt(t *testing.T, root string) {
	t.Helper()
	n := 0
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		n++
		if m := storedActorAt.Find(data); m != nil {
			t.Errorf("%s stores an actor with @: %q", strings.TrimPrefix(p, root), m)
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("scan: %d files, %v", n, err)
	}
}
