package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAuthoredDoc(t *testing.T, projDir, rel, cerneID, body string) {
	t.Helper()
	p := filepath.Join(projDir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\ncerne_id: " + cerneID + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestHookProvenancePushWithOrphanURL (F-0172 crit. 1): a successful push whose
// authored docs cite an unbacked external URL reminds the agent to register the
// source, naming the entity.
func TestHookProvenancePushWithOrphanURL(t *testing.T) {
	projDir, _, _ := setupProject(t)
	writeAuthoredDoc(t, projDir, ".docs/current/features/F-0001-coisa/spec.md", "F-0001",
		"Baseado em https://exemplo.org/artigo-que-moldou.")
	stdin := hookJSON(t, "s", projDir, "Bash",
		map[string]any{"command": "cerne push"},
		map[string]any{"stdout": "Sent: 1 document(s) (snapshot abc)."})
	out, err := execCerneStdin(t, stdin, "hook", "provenance")
	if err != nil {
		t.Fatalf("hook provenance: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"additionalContext", "F-0001", "https://exemplo.org/artigo-que-moldou", "reference new"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

// TestHookProvenancePushCleanIsSilent (F-0172 crit. 2): a push whose docs cite
// no orphan URL (internal host / no URLs) emits nothing — no reminder fatigue.
func TestHookProvenancePushCleanIsSilent(t *testing.T) {
	projDir, _, _ := setupProject(t)
	writeAuthoredDoc(t, projDir, ".docs/current/features/F-0001-coisa/spec.md", "F-0001",
		"Só host interno: http://gitea.local/acme/x e prosa sem URL.")
	stdin := hookJSON(t, "s", projDir, "Bash",
		map[string]any{"command": "cerne push"},
		map[string]any{"stdout": "Sent: 1 document(s)."})
	out, err := execCerneStdin(t, stdin, "hook", "provenance")
	if err != nil {
		t.Fatalf("hook provenance: %v\noutput: %s", err, out)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("want silence, got:\n%s", out)
	}
}

// TestHookProvenanceGateTriggersPreserved (F-0172 crit. 3): feature complete and
// decision new/substituir keep the unconditional reminder; feature approve (a
// human gate the agent never runs) is no longer a trigger; failures and
// malformed payloads are silent (crit. 5).
func TestHookProvenanceGateTriggersPreserved(t *testing.T) {
	projDir, _, _ := setupProject(t)
	for name, tc := range map[string]struct {
		command string
		remind  bool
	}{
		"feature complete":    {"cerne feature complete F-0001", true},
		"decision new":        {`cerne decision new "Usar X"`, true},
		"decision substituir": {"cerne decision substituir D-0001 --by D-0002", true},
		"feature approve":     {"cerne feature approve F-0001", false},
		"unrelated":           {"go test ./...", false},
	} {
		stdin := hookJSON(t, "s", projDir, "Bash",
			map[string]any{"command": tc.command}, map[string]any{"stdout": "ok"})
		out, err := execCerneStdin(t, stdin, "hook", "provenance")
		if err != nil {
			t.Fatalf("%s: errored: %v\noutput: %s", name, err, out)
		}
		if tc.remind && !strings.Contains(out, "reference new") {
			t.Fatalf("%s: want reminder, got:\n%s", name, out)
		}
		if !tc.remind && strings.TrimSpace(out) != "" {
			t.Fatalf("%s: want silence, got:\n%s", name, out)
		}
	}
	if out, err := execCerneStdin(t, "garbage{", "hook", "provenance"); err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("malformed payload: err=%v out=%s", err, out)
	}
}

// TestHookProvenancePushFailedIsSilent: a push that did not send (error or
// nothing to send) triggers no scan.
func TestHookProvenancePushFailedIsSilent(t *testing.T) {
	projDir, _, _ := setupProject(t)
	writeAuthoredDoc(t, projDir, ".docs/current/features/F-0001-coisa/spec.md", "F-0001",
		"https://exemplo.org/orfa")
	stdin := hookJSON(t, "s", projDir, "Bash",
		map[string]any{"command": "cerne push"},
		map[string]any{"stdout": "Nothing to send."})
	out, err := execCerneStdin(t, stdin, "hook", "provenance")
	if err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("err=%v out=%s", err, out)
	}
}

// TestHookProvenanceNeedsAnInvocation (B-0038): the gate reminder fires only
// when the command runs the cerne binary with the gate verb; the words in a
// grep, heredoc, commit message or path are no call.
func TestHookProvenanceNeedsAnInvocation(t *testing.T) {
	projDir, _, _ := setupProject(t)
	for name, tc := range map[string]struct {
		command string
		remind  bool
	}{
		"grep in a cerne path": {`cd /w/cerne && grep -n "decision new" internal/cli/*.go`, false},
		"heredoc body":         {"cd /w/cerne && cat > x_test.go <<'EOF'\ncerne decision new X\nEOF", false},
		"commit message":       {`cd /w/cerne && git commit -m "docs: cerne decision new flow"`, false},
		"unrelated in a repo":  {"cd /w/cerne && go test ./...", false},
		"plain":                {`cerne decision new "X"`, true},
		"flag before verb":     {"./bin/cerne --verbose decision supersede D-0001 --by D-0002", true},
		"after cd":             {"cd x && cerne feature complete B-1", true},
		"env prefix":           {`CERNE_ACTOR=agent cerne decision new "X"`, true},
		"exe":                  {`cerne.exe decision new "X"`, true},
		"after heredoc":        {"cat <<EOF\nx\nEOF\ncerne decision new X", true},
	} {
		stdin := hookJSON(t, "s", projDir, "Bash",
			map[string]any{"command": tc.command}, map[string]any{"stdout": "ok"})
		out, err := execCerneStdin(t, stdin, "hook", "provenance")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := strings.Contains(out, "additionalContext"); got != tc.remind {
			t.Errorf("%s: reminder = %v, want %v\n%s", name, got, tc.remind, out)
		}
	}
}
