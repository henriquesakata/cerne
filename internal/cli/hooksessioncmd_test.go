package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

func sessionPayload(sess, cwd, source string) string {
	p := `{"session_id":` + jsonString(sess) + `,"cwd":` + jsonString(cwd)
	if source != "" {
		p += `,"source":` + jsonString(source)
	}
	return p + "}"
}

// TestHookSessionCompactWithHandoff (F-0368 crit. 12): after a compaction, a
// session bound to an active W- gets the W-, its latest HO (id, state, age,
// next step) and `cerne show HO-…`.
func TestHookSessionCompactWithHandoff(t *testing.T) {
	projDir, _, repoDir := boundWork(t, "sess-c")
	saveHO(t, repoDir, "HO-0004", "W-0001", "rodar a suíte", time.Now().UTC().Add(-3*time.Hour))
	called := failingNetTools(t)
	out, err := execCerneStdin(t, sessionPayload("sess-c", projDir, "compact"), "hook", "session-context")
	if err != nil {
		t.Fatalf("session-context: %v\n%s", err, out)
	}
	for _, want := range []string{"W-0001", "HO-0004", "pending", "3h", "rodar a suíte", "cerne show HO-0004"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if called() {
		t.Fatal("session-context invoked git/ssh")
	}
}

// TestHookSessionCompactWithoutHandoff (crit. 12): no HO for the bound W- →
// the single line telling how to record one.
func TestHookSessionCompactWithoutHandoff(t *testing.T) {
	projDir, _, _ := boundWork(t, "sess-c0")
	out, err := execCerneStdin(t, sessionPayload("sess-c0", projDir, "compact"), "hook", "session-context")
	if err != nil {
		t.Fatalf("session-context: %v\n%s", err, out)
	}
	want := `cerne work handoff W-0001 --next \"…\"` + "` lets the next stretch, or another session, resume by the code."
	if !strings.Contains(out, want) {
		t.Fatalf("output missing %q:\n%s", want, out)
	}
	if strings.Contains(out, "cerne show HO-") {
		t.Fatalf("names an HO that does not exist:\n%s", out)
	}
}

// TestHookSessionCompactUnbound (crit. 12): compact with no binding is the
// plain session context, nothing added.
func TestHookSessionCompactUnbound(t *testing.T) {
	projDir, _, _ := boundWork(t, "sess-other")
	plain, err := execCerneStdin(t, sessionPayload("sess-free", projDir, "startup"), "hook", "session-context")
	if err != nil {
		t.Fatal(err)
	}
	out, err := execCerneStdin(t, sessionPayload("sess-free", projDir, "compact"), "hook", "session-context")
	if err != nil || out != plain {
		t.Fatalf("unbound compact differs from startup (err=%v):\n%s\nwant:\n%s", err, out, plain)
	}
}

// TestHookSessionOtherSourcesGolden (crit. 12): every non-compact source —
// and a payload with none — is byte for byte the output from before F-0368
// (golden captured then), even for a bound session whose W- has an HO.
func TestHookSessionOtherSourcesGolden(t *testing.T) {
	projDir, _, repoDir := boundWork(t, "sess-g")
	saveHO(t, repoDir, "HO-0004", "W-0001", "rodar a suíte", time.Now().UTC().Add(-3*time.Hour))
	w, err := store.LoadWork(repoDir, "W-0001")
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "session_context_noncompact.golden")
	for _, source := range []string{"", "startup", "resume", "clear"} {
		out, err := execCerneStdin(t, sessionPayload("sess-g", projDir, source), "hook", "session-context")
		if err != nil {
			t.Fatalf("%q: %v", source, err)
		}
		out = strings.ReplaceAll(out, w.Actor, "<actor>")
		if os.Getenv("CERNE_UPDATE_GOLDEN") != "" {
			if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if out != string(want) {
			t.Fatalf("source %q differs from the golden:\n%s\nwant:\n%s", source, out, want)
		}
	}
}
