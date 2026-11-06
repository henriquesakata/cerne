package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func sessionStartJSON(t *testing.T, sess, cwd string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"session_id": sess, "cwd": cwd, "hook_event_name": "SessionStart", "source": "startup"})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestHookChannelExportsSession (F-0325): session start exports CERNE_SESSION
// through CLAUDE_ENV_FILE so the agent's own commands (msg, next, status) know
// their session. Only an id that is inert when the file is sourced by bash is
// written, appended after whatever other hooks exported.
func TestHookChannelExportsSession(t *testing.T) {
	projDir, _, _ := setupProject(t)
	env := filepath.Join(t.TempDir(), "env.sh")
	if err := os.WriteFile(env, []byte("export OTHER=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_ENV_FILE", env)

	if out, err := execCerneStdin(t, sessionStartJSON(t, "4f1c-9a_b.2", projDir), "hook", "channel", "--point", "session-start"); err != nil {
		t.Fatalf("hook channel: %v\n%s", err, out)
	}
	for _, bad := range []string{"a;rm -rf x", "a b", "$(id)", "", "x\nexport Y=1"} {
		if out, err := execCerneStdin(t, sessionStartJSON(t, bad, projDir), "hook", "channel", "--point", "session-start"); err != nil {
			t.Fatalf("hook channel %q: %v\n%s", bad, err, out)
		}
	}
	got, err := os.ReadFile(env)
	if err != nil {
		t.Fatal(err)
	}
	if want := "export OTHER=1\nexport CERNE_SESSION=4f1c-9a_b.2\n"; string(got) != want {
		t.Fatalf("env file = %q, want %q", got, want)
	}

	t.Setenv("CLAUDE_ENV_FILE", "")
	if out, err := execCerneStdin(t, sessionStartJSON(t, "s2", projDir), "hook", "channel", "--point", "session-start"); err != nil {
		t.Fatalf("without CLAUDE_ENV_FILE: %v\n%s", err, out)
	}
}
